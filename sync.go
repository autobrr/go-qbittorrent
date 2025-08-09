package qbittorrent

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// SyncManager manages synchronization of MainData updates and provides
// a consistent view of the qBittorrent state across partial updates.
type SyncManager struct {
	client           *Client
	mu               sync.RWMutex
	syncMu           sync.Mutex
	syncCond         *sync.Cond
	syncing          bool
	data             *MainData
	rid              int64
	lastSync         time.Time
	lastSyncDuration time.Duration
	lastError        error
	options          SyncOptions
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
		client:  client,
		options: opts,
	}
	sm.syncCond = sync.NewCond(&sm.syncMu)

	return sm
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
// and return immediately without performing another sync
func (sm *SyncManager) Sync(ctx context.Context) error {
	// First, check if a sync is already in progress
	sm.syncMu.Lock()
	if sm.syncing {
		// Another sync is in progress, wait for it to complete
		sm.syncCond.Wait()
		// Get the cached error from the last sync
		sm.mu.RLock()
		cachedError := sm.lastError
		sm.mu.RUnlock()
		sm.syncMu.Unlock()
		// Return the cached error from the sync that just completed
		return cachedError
	}

	// Mark that we're starting a sync
	sm.syncing = true
	sm.syncMu.Unlock()

	// Ensure we clean up the syncing flag and notify waiting goroutines
	defer func() {
		sm.syncMu.Lock()
		sm.syncing = false
		sm.syncCond.Broadcast() // Wake up all waiting goroutines
		sm.syncMu.Unlock()
	}()

	// Perform the actual sync with timing
	startTime := time.Now()
	var syncError error

	sm.mu.Lock()
	defer func() {
		sm.lastSyncDuration = time.Since(startTime)
		sm.lastSync = time.Now()
		sm.lastError = syncError // Store the error for future cached calls
		sm.mu.Unlock()
	}()

	// Get raw JSON to detect field presence
	rawData, err := sm.getRawMainData(ctx, sm.rid)
	if err != nil {
		syncError = err
		if sm.options.OnError != nil {
			sm.options.OnError(err)
		}
		return err
	}

	// Also get parsed struct for convenience
	source, err := sm.client.SyncMainDataCtx(ctx, sm.rid)
	if err != nil {
		syncError = err
		if sm.options.OnError != nil {
			sm.options.OnError(err)
		}
		return err
	}

	// Check if this is a full update
	isFullUpdate := false
	if fullUpdateVal, exists := rawData["full_update"]; exists {
		if fullUpdate, ok := fullUpdateVal.(bool); ok {
			isFullUpdate = fullUpdate
		}
	}

	if sm.data == nil || isFullUpdate {
		// First sync or full update - replace everything
		sm.data = source
		sm.initializeMaps()
	} else {
		// Partial update - merge intelligently
		sm.mergePartialUpdate(rawData, source)
	}

	sm.rid = source.Rid

	// Call update callback if set
	if sm.options.OnUpdate != nil {
		sm.options.OnUpdate(sm.data)
	}

	// Success - clear any previous error
	syncError = nil
	return nil
}

// getRawMainData fetches raw JSON to detect which fields are present
func (sm *SyncManager) getRawMainData(ctx context.Context, rid int64) (map[string]interface{}, error) {
	resp, err := sm.client.getCtx(ctx, "/sync/maindata", map[string]string{
		"rid": fmt.Sprintf("%d", rid),
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	return raw, nil
}

// initializeMaps ensures all maps are initialized and cleared
func (sm *SyncManager) initializeMaps() {
	if sm.data.Torrents == nil {
		sm.data.Torrents = make(map[string]Torrent)
	} else {
		// Clear existing map to preserve capacity
		clear(sm.data.Torrents)
	}

	if sm.data.Categories == nil {
		sm.data.Categories = make(map[string]Category)
	} else {
		// Clear existing map to preserve capacity
		clear(sm.data.Categories)
	}

	if sm.data.Trackers == nil {
		sm.data.Trackers = make(map[string][]string)
	} else {
		// Clear existing map to preserve capacity
		clear(sm.data.Trackers)
	}

	if sm.data.Tags == nil {
		sm.data.Tags = make([]string, 0)
	} else {
		// Clear slice but preserve capacity
		sm.data.Tags = sm.data.Tags[:0]
	}
}

// mergePartialUpdate efficiently merges partial updates using JSON unmarshaling
func (sm *SyncManager) mergePartialUpdate(rawData map[string]interface{}, source *MainData) {
	// Update RID and server state
	sm.data.Rid = source.Rid
	sm.data.ServerState = source.ServerState

	// Handle torrents with smart JSON merging
	if torrentsRaw, exists := rawData["torrents"]; exists {
		if torrentsMap, ok := torrentsRaw.(map[string]interface{}); ok {
			sm.mergeTorrents(torrentsMap)
		}
	}

	// Remove deleted torrents
	for _, hash := range source.TorrentsRemoved {
		delete(sm.data.Torrents, hash)
	}

	// Handle other fields (these are typically complete updates)
	if len(source.Categories) > 0 {
		for name, category := range source.Categories {
			sm.data.Categories[name] = category
		}
	}
	for _, name := range source.CategoriesRemoved {
		delete(sm.data.Categories, name)
	}

	if len(source.Trackers) > 0 {
		for hash, trackers := range source.Trackers {
			trackersCopy := make([]string, len(trackers))
			copy(trackersCopy, trackers)
			sm.data.Trackers[hash] = trackersCopy
		}
	}

	if len(source.Tags) > 0 {
		sm.data.Tags = append(sm.data.Tags, source.Tags...)
		sm.data.Tags = removeDuplicateStrings(sm.data.Tags)
	}
	if len(source.TagsRemoved) > 0 {
		sm.data.Tags = removeStrings(sm.data.Tags, source.TagsRemoved)
	}
}

// mergeTorrents efficiently merges torrent updates using JSON unmarshaling
func (sm *SyncManager) mergeTorrents(torrentsMap map[string]interface{}) {
	for hash, torrentRaw := range torrentsMap {
		existing, exists := sm.data.Torrents[hash]

		if !exists {
			// New torrent - unmarshal directly
			torrentBytes, _ := json.Marshal(torrentRaw)
			var newTorrent Torrent
			if err := json.Unmarshal(torrentBytes, &newTorrent); err == nil {
				sm.data.Torrents[hash] = newTorrent
			}
			continue
		}

		// Existing torrent - merge partial update
		// Convert existing to map, merge with update, then convert back
		existingBytes, _ := json.Marshal(existing)
		var existingMap map[string]interface{}
		json.Unmarshal(existingBytes, &existingMap)

		// Merge the partial update into existing data
		if updateMap, ok := torrentRaw.(map[string]interface{}); ok {
			for key, value := range updateMap {
				existingMap[key] = value
			}
		}

		// Convert back to Torrent struct
		mergedBytes, _ := json.Marshal(existingMap)
		var mergedTorrent Torrent
		if err := json.Unmarshal(mergedBytes, &mergedTorrent); err == nil {
			sm.data.Torrents[hash] = mergedTorrent
		}
	}
}

// GetData returns a deep copy of the current synchronized data
func (sm *SyncManager) GetData() *MainData {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.data == nil {
		return nil
	}

	// Return a deep copy to prevent external modifications
	return sm.copyMainData(sm.data)
}

// GetTorrents returns a copy of all torrents
func (sm *SyncManager) GetTorrents() map[string]Torrent {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.data == nil {
		return nil
	}

	result := make(map[string]Torrent, len(sm.data.Torrents))
	for k, v := range sm.data.Torrents {
		result[k] = v
	}
	return result
}

// GetTorrent returns a specific torrent by hash
func (sm *SyncManager) GetTorrent(hash string) (Torrent, bool) {
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
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.data == nil {
		return ServerState{}
	}

	return sm.data.ServerState
}

// GetCategories returns a copy of all categories
func (sm *SyncManager) GetCategories() map[string]Category {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.data == nil {
		return nil
	}

	result := make(map[string]Category, len(sm.data.Categories))
	for k, v := range sm.data.Categories {
		result[k] = v
	}
	return result
}

// GetTags returns a copy of all tags
func (sm *SyncManager) GetTags() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.data == nil || len(sm.data.Tags) == 0 {
		return nil
	}

	result := make([]string, len(sm.data.Tags))
	copy(result, sm.data.Tags)
	return result
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
		jitter := time.Duration(rand.Float64() * jitterRange)
		// Apply jitter in both directions (±)
		if rand.Float64() < 0.5 {
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
		Torrents:    make(map[string]Torrent, len(src.Torrents)),
		Categories:  make(map[string]Category, len(src.Categories)),
		Trackers:    make(map[string][]string, len(src.Trackers)),
	}

	for k, v := range src.Torrents {
		dst.Torrents[k] = v
	}

	for k, v := range src.Categories {
		dst.Categories[k] = v
	}

	for k, v := range src.Trackers {
		trackers := make([]string, len(v))
		copy(trackers, v)
		dst.Trackers[k] = trackers
	}

	if len(src.Tags) > 0 {
		dst.Tags = make([]string, len(src.Tags))
		copy(dst.Tags, src.Tags)
	}

	return dst
}

// Helper functions
func removeDuplicateStrings(slice []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(slice))

	for _, item := range slice {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}

	return result
}

func removeStrings(slice, toRemove []string) []string {
	removeMap := make(map[string]bool)
	for _, item := range toRemove {
		removeMap[item] = true
	}

	result := make([]string, 0, len(slice))
	for _, item := range slice {
		if !removeMap[item] {
			result = append(result, item)
		}
	}

	return result
}
