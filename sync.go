package qbittorrent

import (
	"context"
	"maps"
	"math/rand"
	"slices"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// SyncManager manages synchronization of MainData updates and provides
// a consistent view of the qBittorrent state across partial updates.
type SyncManager struct {
	mu               sync.RWMutex
	data             *MainData
	rid              int64
	lastSync         time.Time
	lastSyncDuration time.Duration
	lastError        error
	client           *Client
	trackerManager   *TrackerManager
	syncGroup        singleflight.Group
	options          SyncOptions
	allTorrents      []Torrent
	resultPool       sync.Pool
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
	// OnUpdate is called whenever the data is updated
	OnUpdate func(*MainData)
	// OnError is called when sync encounters an error
	OnError func(error)
	// RetainRemovedData keeps removed items for one sync cycle for comparison
	RetainRemovedData bool
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
	sm.resultPool.New = func() interface{} {
		leak := make([]Torrent, 0, 100)
		return &leak // initial capacity
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

// Sync performs a synchronization with the qBittorrent server
// If another sync is already in progress, this method will wait for it to complete
// and all callers will receive the same result (using singleflight pattern).
// Note: Uses context.Background() for all syncs to avoid context confusion in batched calls.
func (sm *SyncManager) Sync(ctx context.Context) error {
	_, err, _ := sm.syncGroup.Do("sync", func() (interface{}, error) {
		return sm.doSync(ctx)
	})
	return err
}

// doSync performs the actual sync operation (singleflight-compatible signature)
func (sm *SyncManager) doSync(ctx context.Context) (interface{}, error) {
	startTime := time.Now()
	var err error = nil

	defer func() {
		sm.lastSyncDuration = time.Since(startTime)
		sm.lastSync = time.Now()
		sm.lastError = err
		sm.mu.Unlock()
	}()

	// Initialize data if needed
	if sm.data == nil {
		sm.data = &MainData{}
	}

	sm.mu.Lock()
	if err = sm.data.Update(ctx, sm.client); err != nil {
		if sm.options.OnError != nil {
			sm.options.OnError(err)
		}
		return nil, err
	}

	sm.rid = sm.data.Rid
	// Update cached torrent slice
	sm.allTorrents = sm.allTorrents[:0]
	for _, torrent := range sm.data.Torrents {
		sm.allTorrents = append(sm.allTorrents, torrent)
	}

	// Call update callback if set
	if sm.options.OnUpdate != nil {
		sm.options.OnUpdate(sm.copyMainData(sm.data))
	}

	return nil, nil
}

// ensureFreshData checks if data is stale or missing and triggers a non-blocking sync if needed
func (sm *SyncManager) ensureFreshData() {
	// Fast path: check if we just checked freshness very recently (< 100ms)
	// This prevents redundant checks when multiple Get* methods are called in quick succession
	sm.mu.RLock()
	t := time.Now()

	if t.Before(sm.lastSync.Add(5 * time.Millisecond)) {
		// We just checked freshness, no need to check again
		sm.mu.RUnlock()
		return
	}

	// Now check if we actually need to sync
	shouldSync := false
	if sm.data == nil {
		// If data is nil, only sync if DynamicSync is enabled
		shouldSync = sm.options.DynamicSync
	} else if sm.options.DynamicSync {
		// Only check staleness if DynamicSync is enabled
		staleThreshold := sm.calculateStaleThreshold()
		if t.After(sm.lastSync.Add(staleThreshold)) {
			shouldSync = true
		}
	}

	sm.mu.RUnlock()
	// Trigger async sync if needed - don't block the reader
	// singleflight will automatically deduplicate concurrent syncs
	if shouldSync {
		sm.Sync(context.Background())
	}
}

// calculateStaleThreshold determines how old data can be before it's considered stale
func (sm *SyncManager) calculateStaleThreshold() time.Duration {
	// If SyncInterval is set and > 0, use it
	if sm.options.SyncInterval > 0 {
		return sm.options.SyncInterval
	}

	// Otherwise, use dynamic calculation based on last sync duration
	if sm.options.DynamicSync && sm.lastSyncDuration > 0 {
		// Use 2x the last sync duration as threshold, but respect bounds
		dynamicThreshold := sm.lastSyncDuration * 2
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

// GetData returns a deep copy of the current synchronized data
func (sm *SyncManager) GetData() *MainData {
	sm.ensureFreshData()
	return sm.GetDataUnchecked()
}

// GetDataUnchecked returns a deep copy of the current synchronized data without checking freshness.
// This is faster but may return stale data. Use this when you've just called Sync() or when
// AutoSync is enabled and you don't need the absolute latest data.
func (sm *SyncManager) GetDataUnchecked() *MainData {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.data == nil {
		return nil
	}

	// Return a deep copy to prevent external modifications
	return sm.copyMainData(sm.data)
}

// GetTorrents returns a filtered list of torrents
func (sm *SyncManager) GetTorrents(options TorrentFilterOptions) []Torrent {
	sm.ensureFreshData()
	return sm.GetTorrentsUnchecked(options)
}

// GetTorrentsUnchecked returns a filtered list of torrents without checking freshness.
// This is faster but may return stale data. Use this when you've just called Sync() or when
// AutoSync is enabled and you don't need the absolute latest data.
func (sm *SyncManager) GetTorrentsUnchecked(options TorrentFilterOptions) []Torrent {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.data == nil {
		return nil
	}

	// Get a buffer from the pool
	var resultBuffer []Torrent
	if pooled := sm.resultPool.Get(); pooled != nil {
		resultBuffer = (*pooled.(*[]Torrent))[:0]
	} else {
		var length int
		if len(options.Hashes) > 0 {
			length = len(options.Hashes)
		} else {
			length = len(sm.allTorrents) - options.Offset
			if options.Limit != 0 && length > options.Limit {
				length = options.Limit
			}

			if length <= 0 {
				length = 100
			}
		}

		resultBuffer = make([]Torrent, 0, length)
	}

	for _, torrent := range sm.allTorrents {
		if matchesTorrentFilter(torrent, options) {
			resultBuffer = append(resultBuffer, torrent)
		}
	}

	filtered := applyTorrentFilterOptions(resultBuffer, options)
	result := slices.Clone(filtered)
	sm.resultPool.Put(&resultBuffer)
	return result
}

// GetTorrentMap returns a filtered map of torrents keyed by hash
func (sm *SyncManager) GetTorrentMap(options TorrentFilterOptions) map[string]Torrent {
	torrents := sm.GetTorrents(options)
	if torrents == nil {
		return nil
	}
	result := make(map[string]Torrent, len(torrents))
	for _, torrent := range torrents {
		result[torrent.Hash] = torrent
	}
	return result
}

// GetTorrent returns a specific torrent by hash
func (sm *SyncManager) GetTorrent(hash string) (Torrent, bool) {
	sm.ensureFreshData()
	return sm.GetTorrentUnchecked(hash)
}

// GetTorrentUnchecked returns a specific torrent by hash without checking freshness.
// This is faster but may return stale data. Use this when you've just called Sync() or when
// AutoSync is enabled and you don't need the absolute latest data.
func (sm *SyncManager) GetTorrentUnchecked(hash string) (Torrent, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.data == nil {
		return Torrent{}, false
	}

	torrent, exists := sm.data.Torrents[hash]
	return torrent, exists
}

// GetServerState returns the current server state
func (sm *SyncManager) GetServerState() ServerState {
	sm.ensureFreshData()
	return sm.GetServerStateUnchecked()
}

// GetServerStateUnchecked returns the current server state without checking freshness.
// This is faster but may return stale data.
func (sm *SyncManager) GetServerStateUnchecked() ServerState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.data == nil {
		return ServerState{}
	}

	return sm.data.ServerState
}

// GetCategories returns a copy of all categories
func (sm *SyncManager) GetCategories() map[string]Category {
	sm.ensureFreshData()
	return sm.GetCategoriesUnchecked()
}

// GetCategoriesUnchecked returns a copy of all categories without checking freshness.
// This is faster but may return stale data.
func (sm *SyncManager) GetCategoriesUnchecked() map[string]Category {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.data == nil {
		return nil
	}

	return maps.Clone(sm.data.Categories)
}

// GetTags returns a copy of all tags
func (sm *SyncManager) GetTags() []string {
	sm.ensureFreshData()
	return sm.GetTagsUnchecked()
}

// GetTagsUnchecked returns a copy of all tags without checking freshness.
// This is faster but may return stale data.
func (sm *SyncManager) GetTagsUnchecked() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.data == nil || len(sm.data.Tags) == 0 {
		return nil
	}

	return slices.Clone(sm.data.Tags)
}

// LastSyncTime returns the time of the last successful sync
func (sm *SyncManager) LastSyncTime() time.Time {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return sm.lastSync
}

// LastSyncDuration returns the duration of the last sync operation
func (sm *SyncManager) LastSyncDuration() time.Duration {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return sm.lastSyncDuration
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
	sm.mu.RLock()
	lastDuration := sm.lastSyncDuration
	sm.mu.RUnlock()

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

// copyMainData creates a deep copy of MainData
func (sm *SyncManager) copyMainData(src *MainData) *MainData {
	dst := &MainData{
		Rid:         src.Rid,
		FullUpdate:  src.FullUpdate,
		ServerState: src.ServerState,
		Torrents:    maps.Clone(src.Torrents),
		Categories:  maps.Clone(src.Categories),
		Trackers:    maps.Clone(src.Trackers),
		Tags:        slices.Clone(src.Tags),
	}

	return dst
}
