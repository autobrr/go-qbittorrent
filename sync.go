package qbittorrent

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math/rand"
	"runtime/debug"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
)

// SyncManager manages synchronization of MainData updates and provides
// a consistent view of the qBittorrent state across partial updates.
type SyncManager struct {
	mu                 sync.RWMutex
	data               *MainData // holds no torrents: they are in snapshot
	snapshot           atomic.Pointer[SyncSnapshot]
	clock              atomic.Pointer[syncClock] // nil until the first sync attempt ends
	lastSuccessfulSync time.Time
	lastError          error
	client             *Client
	trackerManager     *TrackerManager
	syncGroup          singleflight.Group
	bgMu               sync.Mutex
	bgDone             chan struct{} // closed when the background sync ends; nil when none runs
	options            SyncOptions
	seal               *snapshotSeal // nil unless SealSnapshots is on; only doSync uses it
}

// syncClock is the time and the duration of the last sync attempt.
type syncClock struct {
	last     time.Time
	duration time.Duration
}

// SyncOptions configures the behavior of the sync manager
type SyncOptions struct {
	// AutoSync enables automatic periodic syncing
	AutoSync bool
	// SyncInterval is the base interval between automatic syncs (default: 2s)
	SyncInterval time.Duration
	// DynamicSync enables dynamic sync intervals based on request duration
	DynamicSync bool
	// MaxSyncInterval is the maximum sync interval when using dynamic sync (default: 30s)
	MaxSyncInterval time.Duration
	// MinSyncInterval is the minimum sync interval when using dynamic sync (default: 1s)
	MinSyncInterval time.Duration
	// JitterPercent adds randomness to sync intervals (0-100, default: 10)
	JitterPercent int
	// OnUpdate is called after each sync response that changed the data,
	// with a copy of all data. MainData.Changes tells what changed.
	// The manager copies all torrents for it. Prefer OnSyncUpdate.
	OnUpdate func(*MainData)
	// OnSyncUpdate is called after each sync response that changed the data,
	// right after OnUpdate. It gets the Sync snapshot of the sync, and the
	// manager copies no torrent for it.
	OnSyncUpdate func(SyncUpdate)
	// OnError is called when sync encounters an error, and with each error
	// from BeforeFullUpdate
	OnError func(error)
	// BeforeFullUpdate is called with each sync response that has full_update
	// set, before the response enters the cache. qBittorrent sends one after
	// each restart, so use it to read state that a restart can change, such as
	// the Web API version. It runs without the lock of the sync manager, and a cache read meanwhile
	// returns the old data. Use the *Unchecked getters inside it: a checked
	// getter can wait for the sync that runs the callback.
	// ctx has the deadline of the whole sync, which leaves time for a request.
	// An error goes to OnError, and the update still enters the cache. The
	// sync still succeeds, so LastError does not change.
	// The callback must not change or keep update.
	BeforeFullUpdate func(ctx context.Context, update *MainData) error
	// OnTorrentsLeftOut is called with the hashes that a sync response left
	// out of the cache (see MainDataChanges.TorrentsLeftOut). The next sync
	// asks for a full update, which brings them in.
	OnTorrentsLeftOut func(hashes []string)
	// RetainRemovedData keeps removed items for one sync cycle for comparison
	RetainRemovedData bool
	// SealSnapshots makes each sync check that no torrent of the last Sync
	// snapshot changed after the manager published it. When one changed,
	// the sync panics with its hash. The check reads each torrent at each
	// sync, so turn it on in tests and development builds.
	SealSnapshots bool
}

// DefaultSyncOptions returns sensible default options
func DefaultSyncOptions() SyncOptions {
	return SyncOptions{
		AutoSync:          false,
		SyncInterval:      2 * time.Second,
		DynamicSync:       true,
		MaxSyncInterval:   30 * time.Second,
		MinSyncInterval:   1 * time.Second,
		JitterPercent:     10,
		RetainRemovedData: false,
	}
}

// NewSyncManager creates a new sync manager for the given client
func NewSyncManager(client *Client, options ...SyncOptions) *SyncManager {
	opts := DefaultSyncOptions()
	if len(options) > 0 {
		opts = options[0]
	}

	if opts.SyncInterval == 0 {
		opts.SyncInterval = 2 * time.Second
	}

	sm := &SyncManager{
		client:         client,
		options:        opts,
		trackerManager: NewTrackerManager(client),
	}
	if opts.SealSnapshots {
		sm.seal = newSnapshotSeal()
	}

	return sm
}

// Trackers returns the tracker manager associated with this sync manager.
func (sm *SyncManager) Trackers() *TrackerManager {
	if sm == nil {
		return nil
	}
	return sm.trackerManager
}

// Start initializes the sync manager and optionally starts auto-sync
func (sm *SyncManager) Start(ctx context.Context) error {
	// Perform initial full sync
	if err := sm.Sync(ctx); err != nil {
		return err
	}

	// Start auto-sync if enabled
	if sm.options.AutoSync {
		go sm.autoSync(ctx)
	}

	return nil
}

// Sync performs a synchronization with the qBittorrent server.
// Concurrent callers share one sync (singleflight pattern) and receive its result.
// The shared sync ignores the cancellation of the caller that started it, so one
// caller's deadline cannot fail the sync for the others. Each caller stops
// waiting and returns ctx.Err() when its own ctx ends.
// The shared sync runs until it ends or reaches its own deadline (see
// startSync), so OnUpdate or OnError can run after Sync returns. A panic in
// OnUpdate or OnError comes back to each caller that still waits.
func (sm *SyncManager) Sync(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case res := <-sm.startSync(ctx):
		return syncResult(res)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// startSync starts a shared sync, or joins the one in progress.
// The shared sync has a deadline of retryAttempts × (2 × attempt timeout +
// retry delay): each attempt can need a login request before the sync request.
// A full update has a shorter deadline (see doSync).
// Without it, an http.Client with no Timeout could hang the sync
// forever, and every later caller would join the stuck call.
func (sm *SyncManager) startSync(ctx context.Context) <-chan singleflight.Result {
	return sm.syncGroup.DoChan("sync", func() (_ any, err error) {
		// On a panic, DoChan stops the process. Give the panic to the callers instead.
		defer func() {
			if r := recover(); r != nil {
				err = &syncPanic{value: r, stack: debug.Stack()}
			}
		}()
		c := sm.client
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Duration(c.retryAttempts)*(2*c.attemptTimeout()+c.retryDelay))
		defer cancel()
		return sm.doSync(ctx)
	})
}

// syncPanic carries a panic out of the shared sync. Each caller that waits for
// the result panics with it again, as singleflight's Do does, so that the
// caller can recover it.
type syncPanic struct {
	value any
	stack []byte
}

func (p *syncPanic) Error() string {
	return fmt.Sprintf("sync panic: %v\n%s", p.value, p.stack)
}

func syncResult(res singleflight.Result) error {
	if p, ok := errors.AsType[*syncPanic](res.Err); ok {
		panic(p)
	}
	return res.Err
}

// doSync performs the actual sync operation (singleflight-compatible signature).
// Concurrent Sync callers are collapsed by the singleflight group, so doSync
// never runs concurrently with itself and the network fetch needs no lock.
// Keeping the mutex out of the fetch is load-bearing: holding it across the
// round-trip parks every reader (including the *Unchecked getters) for the
// full request duration whenever the server responds slowly.
func (sm *SyncManager) doSync(ctx context.Context) (interface{}, error) {
	startTime := time.Now()

	if sm.seal != nil {
		sm.seal.verify()
	}

	sm.mu.RLock()
	var rid int64
	if sm.data != nil {
		rid = sm.data.Rid
	}
	sm.mu.RUnlock()

	// A full update is the heaviest request. When it times out, a retry sends
	// it again to a server that is already slow. Thus a full update gets one
	// attempt timeout, which leaves no time for a retry after a timeout. The
	// rid read here decides, because a failed sync resets it to 0.
	fetchCtx := ctx
	if rid == 0 {
		var cancel context.CancelFunc
		fetchCtx, cancel = context.WithTimeout(ctx, sm.client.attemptTimeout())
		defer cancel()
	}

	source, rawData, err := sm.client.SyncMainDataCtxWithRaw(fetchCtx, rid)

	// The callback gets the sync deadline, not the one-attempt deadline of the
	// fetch, which the fetch can have used up.
	var cbErr error
	if err == nil && source.FullUpdate && sm.options.BeforeFullUpdate != nil {
		cbErr = sm.options.BeforeFullUpdate(ctx, source)
	}

	// The snapshot is built without the lock: only this sync writes it, and
	// readers keep the published one until applySyncResult swaps it.
	var next *SyncSnapshot
	var changes MainDataChanges
	if err == nil {
		next, changes = buildSnapshot(sm.snapshot.Load(), source, rawData)
		if sm.seal != nil {
			sm.seal.record(next)
		}
	}

	update, syncUpdate, leftOut := sm.applySyncResult(source, rawData, next, changes, startTime, err)

	// Report the callback error after the update enters the cache, so that
	// a checked getter in OnError does not wait for this sync.
	if cbErr != nil && sm.options.OnError != nil {
		sm.options.OnError(fmt.Errorf("before full update: %w", cbErr))
	}

	// Callbacks run after the mutex is released so implementations can safely
	// read back from the sync manager (the *Unchecked getters, LastError,
	// LastSuccessfulSyncTime, ...): a callback invoked under the lock that
	// re-entered any getter self-deadlocked on the non-reentrant RWMutex.
	// Calling Sync from a callback blocks until its ctx ends: it would join
	// the singleflight call it is running in. Checked getters do not block in
	// OnUpdate or OnError, because the sync clock is always set by then.
	if err != nil {
		if sm.options.OnError != nil {
			sm.options.OnError(err)
		}
		return nil, err
	}
	if len(leftOut) > 0 && sm.options.OnTorrentsLeftOut != nil {
		sm.options.OnTorrentsLeftOut(leftOut)
	}
	if update != nil && sm.options.OnUpdate != nil {
		update.Torrents = next.torrentMap()
		sm.options.OnUpdate(update)
	}
	if syncUpdate != nil && sm.options.OnSyncUpdate != nil {
		sm.options.OnSyncUpdate(*syncUpdate)
	}

	return nil, nil
}

// buildSnapshot builds the snapshot that follows prev after a sync response,
// and tells what the response changed in the torrents.
func buildSnapshot(prev *SyncSnapshot, source *MainData, rawData map[string]any) (*SyncSnapshot, MainDataChanges) {
	if source.FullUpdate {
		next, removed := prev.nextFull(source.Torrents)
		changed := make([]string, len(next.torrents))
		for i, t := range next.torrents {
			changed[i] = t.Hash
		}
		return next, MainDataChanges{Torrents: changed, TorrentsRemoved: removed}
	}

	updates, _ := rawData["torrents"].(map[string]any)
	next, changed, leftOut := prev.nextPartial(updates, source.TorrentsRemoved)
	changes := MainDataChanges{Torrents: changed, TorrentsLeftOut: leftOut}
	if len(source.TorrentsRemoved) > 0 {
		changes.TorrentsRemoved = source.TorrentsRemoved
	}
	return next, changes
}

// applySyncResult merges a fetched sync response, publishes next, and updates
// the sync clocks under a brief write lock. changes tells what the response
// changed in the torrents. When the response changed the data, it returns a
// copy of the merged data without the torrents for OnUpdate, and the update
// for OnSyncUpdate, each only when that callback is set. It also returns the
// hashes that the merge left out of the snapshot.
func (sm *SyncManager) applySyncResult(source *MainData, rawData map[string]any, next *SyncSnapshot, changes MainDataChanges, startTime time.Time, syncErr error) (update *MainData, syncUpdate *SyncUpdate, leftOut []string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Initialize even when the sync failed: a nil sm.data would make
	// the *Unchecked getters return nil for an instance that is known to be down.
	if sm.data == nil {
		sm.data = &MainData{}
	}

	now := time.Now()
	if syncErr == nil {
		sm.data.applySyncExceptTorrents(source, rawData, changes)
		leftOut = slices.Clone(changes.TorrentsLeftOut)
		next.time = now
		if prev := sm.snapshot.Load(); prev != nil {
			next.number = prev.number + 1
		} else {
			next.number = 1
		}
		sm.snapshot.Store(next)
		if !sm.data.Changes.empty() {
			if sm.options.OnUpdate != nil {
				update = sm.copyMainData(sm.data)
			}
			if sm.options.OnSyncUpdate != nil {
				syncUpdate = &SyncUpdate{
					Snapshot:    next,
					Changes:     cloneChanges(sm.data.Changes),
					ServerState: sm.data.ServerState,
					Categories:  cloneCategories(sm.data.Categories),
					Tags:        slices.Clone(sm.data.Tags),
					Trackers:    maps.Clone(sm.data.Trackers),
				}
			}
		}
	} else {
		// qBittorrent can count a lost response as sent. Before 5.3.0rc1, it then
		// drops changes when the client sends the old rid again. Thus ask for a
		// full update. The cached data stays until the full update arrives.
		sm.data.Rid = 0
	}

	// The clock records every attempt (used internally to pace syncs), but
	// lastSuccessfulSync only advances when the data actually updated, so
	// callers can tell how fresh the cached data really is during a failure.
	sm.clock.Store(&syncClock{last: now, duration: now.Sub(startTime)})
	if syncErr == nil {
		sm.lastSuccessfulSync = now
	}
	sm.lastError = syncErr
	return update, syncUpdate, leftOut
}

// ensureFreshData starts a sync when the data is stale or missing.
// Stale data is returned at once while the sync runs in the background.
// Missing data makes the caller wait for the sync, but for no longer than
// the client timeout (one request attempt).
func (sm *SyncManager) ensureFreshData() {
	// It takes no lock, so a read does not wait for a sync that applies.
	clock := sm.clock.Load()
	coldCache := clock == nil
	t := time.Now()

	// Fast path: a sync attempt ended very recently. This prevents redundant
	// checks when multiple Get* methods are called in quick succession.
	if !coldCache && t.Before(clock.last.Add(5*time.Millisecond)) {
		return
	}

	// Now check if we actually need to sync
	shouldSync := false
	if coldCache {
		// Before the first sync attempt, only sync if DynamicSync is enabled
		shouldSync = sm.options.DynamicSync
	} else if sm.options.DynamicSync {
		// Only check staleness if DynamicSync is enabled
		if t.After(clock.last.Add(sm.calculateStaleThreshold(clock.duration))) {
			shouldSync = true
		}
	}

	if !shouldSync {
		return
	}
	done := sm.backgroundSync()
	if !coldCache {
		return
	}
	select {
	case <-done:
	case <-time.After(sm.client.attemptTimeout()):
	}
}

// backgroundSync starts a shared sync for the checked getters, or returns
// the done channel of the one they already started. Each join of the shared
// sync keeps a result channel until the sync ends, so the getters join it
// only once per sync, however many reads arrive while it runs.
func (sm *SyncManager) backgroundSync() <-chan struct{} {
	sm.bgMu.Lock()
	defer sm.bgMu.Unlock()
	if sm.bgDone != nil {
		return sm.bgDone
	}
	done := make(chan struct{})
	sm.bgDone = done
	res := sm.startSync(context.Background())
	go func() {
		// singleflight removes the call before it sends the result, so a
		// read after this point starts a new sync.
		r := <-res
		sm.bgMu.Lock()
		sm.bgDone = nil
		sm.bgMu.Unlock()
		close(done)
		// No caller waits for this result, so a panic in a callback stops the process.
		_ = syncResult(r)
	}()
	return done
}

// calculateStaleThreshold determines how old data can be before it's considered stale
func (sm *SyncManager) calculateStaleThreshold(lastSyncDuration time.Duration) time.Duration {
	// If SyncInterval is set and > 0, use it
	if sm.options.SyncInterval > 0 {
		return sm.options.SyncInterval
	}

	// Otherwise, use dynamic calculation based on last sync duration
	if sm.options.DynamicSync && lastSyncDuration > 0 {
		// Use 2x the last sync duration as threshold, but respect bounds
		dynamicThreshold := lastSyncDuration * 2
		if dynamicThreshold < sm.options.MinSyncInterval {
			dynamicThreshold = sm.options.MinSyncInterval
		}
		if dynamicThreshold > sm.options.MaxSyncInterval {
			dynamicThreshold = sm.options.MaxSyncInterval
		}
		return dynamicThreshold
	}

	// Fallback to MinSyncInterval or default
	if sm.options.MinSyncInterval > 0 {
		return sm.options.MinSyncInterval
	}

	// Ultimate fallback
	return 2 * time.Second
}

// Snapshot returns the Sync snapshot of the last sync that the manager
// applied. Freshness works as in [SyncManager.GetData]: it can start a sync,
// and before the first sync ends, it waits for that sync for up to one request
// timeout. It returns nil until a sync succeeded. It does not wait for the
// lock of a sync.
func (sm *SyncManager) Snapshot() *SyncSnapshot {
	sm.ensureFreshData()
	return sm.SnapshotUnchecked()
}

// SnapshotUnchecked returns the Sync snapshot of the last sync that the
// manager applied, or nil until a sync succeeded. Unlike Snapshot, it never
// starts a sync. It takes no lock and sends no request.
func (sm *SyncManager) SnapshotUnchecked() *SyncSnapshot {
	return sm.snapshot.Load()
}

// GetData returns a deep copy of the current synchronized data.
// It returns the cached data at once. When DynamicSync is on and the data is
// stale, it also starts a sync in the background. Before the first sync ends,
// it waits for that sync for up to one request timeout and can return nil;
// LastSyncTime().IsZero() reports that case.
func (sm *SyncManager) GetData() *MainData {
	sm.ensureFreshData()
	return sm.GetDataUnchecked()
}

// GetDataUnchecked returns a deep copy of the current synchronized data without checking freshness.
// Unlike GetData, it never starts a sync. Use this when you've just called Sync() or when
// AutoSync is enabled and you don't need the absolute latest data.
func (sm *SyncManager) GetDataUnchecked() *MainData {
	sm.mu.RLock()
	if sm.data == nil {
		sm.mu.RUnlock()
		return nil
	}
	data := sm.copyMainData(sm.data)
	snapshot := sm.snapshot.Load()
	sm.mu.RUnlock()

	if snapshot != nil {
		data.Torrents = snapshot.torrentMap()
	}
	return data
}

// currentSnapshot returns the snapshot of the last sync, and whether a sync
// attempt ended. The snapshot is nil when no sync succeeded.
func (sm *SyncManager) currentSnapshot() (snapshot *SyncSnapshot, attempted bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.snapshot.Load(), sm.data != nil
}

// GetTorrents returns a filtered list of torrents.
// Freshness works as in [SyncManager.GetData].
func (sm *SyncManager) GetTorrents(options TorrentFilterOptions) []Torrent {
	sm.ensureFreshData()
	return sm.GetTorrentsUnchecked(options)
}

// GetTorrentsUnchecked returns a filtered list of torrents without checking freshness.
// Unlike GetTorrents, it never starts a sync. Use this when you've just called Sync() or when
// AutoSync is enabled and you don't need the absolute latest data.
func (sm *SyncManager) GetTorrentsUnchecked(options TorrentFilterOptions) []Torrent {
	snapshot, attempted := sm.currentSnapshot()
	if !attempted {
		return nil
	}
	if snapshot == nil {
		return []Torrent{}
	}
	return copyTorrents(snapshot.filter(options))
}

// GetTorrentsMatching returns copies of the torrents for which match returns true.
// Freshness works as in [SyncManager.GetData], and match works as in
// [SyncManager.GetTorrentsMatchingUnchecked].
func (sm *SyncManager) GetTorrentsMatching(match func(*Torrent) bool) []Torrent {
	sm.ensureFreshData()
	return sm.GetTorrentsMatchingUnchecked(match)
}

// GetTorrentsMatchingUnchecked returns copies of the torrents for which match
// returns true, without checking freshness. It copies only the matches. It
// returns nil only when the manager has no data yet, and an empty slice when
// no torrent matches.
//
// match gets a torrent of the current Sync snapshot. It must not keep or
// change the pointer.
func (sm *SyncManager) GetTorrentsMatchingUnchecked(match func(*Torrent) bool) []Torrent {
	snapshot, attempted := sm.currentSnapshot()
	if !attempted {
		return nil
	}
	if snapshot == nil {
		return []Torrent{}
	}
	return copyTorrents(snapshot.Matching(match))
}

// GetTorrentMap returns a filtered map of torrents keyed by hash.
// Freshness works as in [SyncManager.GetData].
func (sm *SyncManager) GetTorrentMap(options TorrentFilterOptions) map[string]Torrent {
	sm.ensureFreshData()
	snapshot, attempted := sm.currentSnapshot()
	if !attempted {
		return nil
	}
	if snapshot == nil {
		return map[string]Torrent{}
	}
	refs := snapshot.filter(options)
	result := make(map[string]Torrent, len(refs))
	for _, t := range refs {
		result[t.Hash] = *t
	}
	return result
}

// GetTorrent returns a specific torrent by hash.
// Freshness works as in [SyncManager.GetData].
func (sm *SyncManager) GetTorrent(hash string) (Torrent, bool) {
	sm.ensureFreshData()
	return sm.GetTorrentUnchecked(hash)
}

// GetTorrentUnchecked returns a specific torrent by hash without checking freshness.
// Unlike GetTorrent, it never starts a sync. Use this when you've just called Sync() or when
// AutoSync is enabled and you don't need the absolute latest data.
func (sm *SyncManager) GetTorrentUnchecked(hash string) (Torrent, bool) {
	snapshot := sm.snapshot.Load()
	if snapshot == nil {
		return Torrent{}, false
	}
	torrent, ok := snapshot.Torrent(hash)
	if !ok {
		return Torrent{}, false
	}
	return *torrent, true
}

// GetServerState returns the current server state.
// Freshness works as in [SyncManager.GetData].
func (sm *SyncManager) GetServerState() ServerState {
	sm.ensureFreshData()
	return sm.GetServerStateUnchecked()
}

// GetServerStateUnchecked returns the current server state without checking freshness.
// Unlike GetServerState, it never starts a sync.
func (sm *SyncManager) GetServerStateUnchecked() ServerState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.data == nil {
		return ServerState{}
	}

	return sm.data.ServerState
}

// GetCategories returns a copy of all categories.
// Freshness works as in [SyncManager.GetData].
func (sm *SyncManager) GetCategories() map[string]Category {
	sm.ensureFreshData()
	return sm.GetCategoriesUnchecked()
}

// GetCategoriesUnchecked returns a copy of all categories without checking freshness.
// Unlike GetCategories, it never starts a sync.
func (sm *SyncManager) GetCategoriesUnchecked() map[string]Category {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.data == nil {
		return nil
	}

	return cloneCategories(sm.data.Categories)
}

// GetTrackers returns a copy of the tracker URL to torrent hashes map.
// Freshness works as in [SyncManager.GetData].
// The hash slices alias the cached data; callers must not modify them.
func (sm *SyncManager) GetTrackers() map[string][]string {
	sm.ensureFreshData()
	return sm.GetTrackersUnchecked()
}

// GetTrackersUnchecked returns a copy of the tracker URL to torrent hashes map
// without checking freshness. Unlike GetTrackers, it never starts a sync.
// The hash slices alias the cached data; callers must not modify them.
func (sm *SyncManager) GetTrackersUnchecked() map[string][]string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.data == nil {
		return nil
	}

	return maps.Clone(sm.data.Trackers)
}

// GetTags returns a copy of all tags.
// Freshness works as in [SyncManager.GetData].
func (sm *SyncManager) GetTags() []string {
	sm.ensureFreshData()
	return sm.GetTagsUnchecked()
}

// GetTagsUnchecked returns a copy of all tags without checking freshness.
// Unlike GetTags, it never starts a sync.
func (sm *SyncManager) GetTagsUnchecked() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.data == nil || len(sm.data.Tags) == 0 {
		return nil
	}

	return slices.Clone(sm.data.Tags)
}

// LastSyncTime returns the time of the last sync attempt, whether it succeeded
// or failed. To know how fresh the cached data actually is, use
// LastSuccessfulSyncTime instead.
func (sm *SyncManager) LastSyncTime() time.Time {
	if clock := sm.clock.Load(); clock != nil {
		return clock.last
	}
	return time.Time{}
}

// LastSuccessfulSyncTime returns the time of the last sync that completed
// without error. Unlike LastSyncTime it does not advance on failed syncs, so it
// is the reliable signal for how stale the cached data has become while syncs
// are timing out or failing.
func (sm *SyncManager) LastSuccessfulSyncTime() time.Time {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return sm.lastSuccessfulSync
}

// LastSyncDuration returns the duration of the last sync operation
func (sm *SyncManager) LastSyncDuration() time.Duration {
	if clock := sm.clock.Load(); clock != nil {
		return clock.duration
	}
	return 0
}

// LastError returns the error from the last sync operation, or nil if successful
func (sm *SyncManager) LastError() error {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return sm.lastError
}

// autoSync runs the automatic sync loop with dynamic intervals
func (sm *SyncManager) autoSync(ctx context.Context) {
	interval := sm.options.SyncInterval

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
			_ = sm.Sync(ctx)

			// Calculate next interval based on sync options
			if sm.options.DynamicSync {
				interval = sm.calculateNextInterval()
			} else {
				interval = sm.options.SyncInterval
			}
		}
	}
}

// calculateNextInterval determines the next sync interval based on the last sync duration
func (sm *SyncManager) calculateNextInterval() time.Duration {
	lastDuration := sm.LastSyncDuration()

	// Base interval is double the last sync duration
	baseInterval := lastDuration * 2

	// Apply bounds
	if baseInterval < sm.options.MinSyncInterval {
		baseInterval = sm.options.MinSyncInterval
	}
	if baseInterval > sm.options.MaxSyncInterval {
		baseInterval = sm.options.MaxSyncInterval
	}

	// Add jitter to prevent thundering herd
	if sm.options.JitterPercent > 0 && sm.options.JitterPercent <= 100 {
		jitterRange := float64(baseInterval) * float64(sm.options.JitterPercent) / 100.0
		// Use a single random value for both magnitude and direction
		randVal := rand.Float64()
		jitter := time.Duration(randVal * jitterRange)
		// Use the fractional part to determine direction (< 0.5 = add, >= 0.5 = subtract)
		if randVal < 0.5 {
			baseInterval += jitter
		} else {
			baseInterval -= jitter
		}

		// Ensure we don't go below minimum after jitter
		if baseInterval < sm.options.MinSyncInterval {
			baseInterval = sm.options.MinSyncInterval
		}
	}

	return baseInterval
}

// cloneCategories copies categories along with each download path, so a
// caller that changes the copy cannot change the cached category.
func cloneCategories(src map[string]Category) map[string]Category {
	if src == nil {
		return nil
	}
	dst := make(map[string]Category, len(src))
	for name, category := range src {
		if category.DownloadPath != nil {
			downloadPath := *category.DownloadPath
			category.DownloadPath = &downloadPath
		}
		dst[name] = category
	}
	return dst
}

// copyMainData creates a deep copy of MainData
func (sm *SyncManager) copyMainData(src *MainData) *MainData {
	dst := &MainData{
		Rid:         src.Rid,
		FullUpdate:  src.FullUpdate,
		ServerState: src.ServerState,
		Torrents:    maps.Clone(src.Torrents),
		Categories:  cloneCategories(src.Categories),
		Trackers:    maps.Clone(src.Trackers),
		Tags:        slices.Clone(src.Tags),
		Changes:     src.Changes,
	}
	dst.Changes = cloneChanges(src.Changes)

	return dst
}

func cloneChanges(c MainDataChanges) MainDataChanges {
	c.Torrents = slices.Clone(c.Torrents)
	c.TorrentsRemoved = slices.Clone(c.TorrentsRemoved)
	c.TorrentsLeftOut = slices.Clone(c.TorrentsLeftOut)
	return c
}
