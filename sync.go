package qbittorrent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"sort"
	"strings"
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

	// Get both raw JSON and parsed struct in a single request
	rawData, source, err := sm.getSyncData(ctx, sm.rid)
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
		// Ensure maps are initialized for future partial updates
		if sm.data.Torrents == nil {
			sm.data.Torrents = make(map[string]Torrent)
		}
		if sm.data.Categories == nil {
			sm.data.Categories = make(map[string]Category)
		}
		if sm.data.Trackers == nil {
			sm.data.Trackers = make(map[string][]string)
		}
		if sm.data.Tags == nil {
			sm.data.Tags = make([]string, 0)
		}
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

// getSyncData fetches and parses sync data in a single request
func (sm *SyncManager) getSyncData(ctx context.Context, rid int64) (map[string]interface{}, *MainData, error) {
	resp, err := sm.client.getCtx(ctx, "/sync/maindata", map[string]string{
		"rid": fmt.Sprintf("%d", rid),
	})
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	// Read the response body once
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	// Parse raw JSON for field detection
	var rawData map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &rawData); err != nil {
		return nil, nil, err
	}

	// Parse structured data
	var source MainData
	if err := json.Unmarshal(bodyBytes, &source); err != nil {
		return nil, nil, err
	}

	// Populate hash fields from map keys since JSON doesn't include hash in the object
	for hash, torrent := range source.Torrents {
		torrent.Hash = hash
		source.Torrents[hash] = torrent
	}

	return rawData, &source, nil
}

// mergePartialUpdate efficiently merges partial updates using field-level merging
func (sm *SyncManager) mergePartialUpdate(rawData map[string]interface{}, source *MainData) {
	// Update RID and server state
	sm.data.Rid = source.Rid
	sm.data.ServerState = source.ServerState

	// Handle torrents ONLY if the torrents field is present in the raw JSON
	// This prevents clearing torrents when there's no torrent update
	if torrentsRaw, exists := rawData["torrents"]; exists {
		if torrentsMap, ok := torrentsRaw.(map[string]interface{}); ok {
			sm.mergeTorrentsPartial(torrentsMap)
		}
	}

	// Remove deleted torrents ONLY if there are actually items to remove
	if len(source.TorrentsRemoved) > 0 {
		remove(source.TorrentsRemoved, &sm.data.Torrents)
	}

	// Handle categories ONLY if present in raw JSON
	if categoriesRaw, exists := rawData["categories"]; exists {
		if _, ok := categoriesRaw.(map[string]interface{}); ok {
			merge(source.Categories, &sm.data.Categories)
		}
	}
	if len(source.CategoriesRemoved) > 0 {
		remove(source.CategoriesRemoved, &sm.data.Categories)
	}

	// Handle trackers ONLY if present in raw JSON
	if trackersRaw, exists := rawData["trackers"]; exists {
		if _, ok := trackersRaw.(map[string]interface{}); ok {
			merge(source.Trackers, &sm.data.Trackers)
		}
	}

	// Handle tags ONLY if present in raw JSON
	if tagsRaw, exists := rawData["tags"]; exists {
		if _, ok := tagsRaw.([]interface{}); ok {
			mergeSlice(source.Tags, &sm.data.Tags)
		}
	}
	if len(source.TagsRemoved) > 0 {
		removeSlice(source.TagsRemoved, &sm.data.Tags)
	}
}

// mergeTorrentsPartial merges only the fields that are present in the update
func (sm *SyncManager) mergeTorrentsPartial(torrentsMap map[string]interface{}) {
	for hash, torrentRaw := range torrentsMap {
		updateMap, ok := torrentRaw.(map[string]interface{})
		if !ok {
			continue
		}

		existing, exists := sm.data.Torrents[hash]
		if !exists {
			// New torrent - create a minimal torrent with the hash at least
			existing = Torrent{Hash: hash}
		}

		// Always start with existing data and update only provided fields
		sm.updateTorrentFields(&existing, updateMap)
		sm.data.Torrents[hash] = existing
	}
}

// updateTorrentFields updates only the fields that are present in the update map
func (sm *SyncManager) updateTorrentFields(torrent *Torrent, updateMap map[string]interface{}) {
	// Only update fields that are explicitly present in the JSON
	if val, exists := updateMap["name"]; exists {
		if name, ok := val.(string); ok {
			torrent.Name = name
		}
	}
	if val, exists := updateMap["hash"]; exists {
		if hash, ok := val.(string); ok {
			torrent.Hash = hash
		}
	}
	if val, exists := updateMap["progress"]; exists {
		if progress, ok := val.(float64); ok {
			torrent.Progress = progress
		}
	}
	if val, exists := updateMap["dlspeed"]; exists {
		if speed, ok := val.(float64); ok {
			torrent.DlSpeed = int64(speed)
		}
	}
	if val, exists := updateMap["upspeed"]; exists {
		if speed, ok := val.(float64); ok {
			torrent.UpSpeed = int64(speed)
		}
	}
	if val, exists := updateMap["state"]; exists {
		if state, ok := val.(string); ok {
			torrent.State = TorrentState(state)
		}
	}
	if val, exists := updateMap["category"]; exists {
		if category, ok := val.(string); ok {
			torrent.Category = category
		}
	}
	if val, exists := updateMap["tags"]; exists {
		if tags, ok := val.(string); ok {
			torrent.Tags = tags
		}
	}
	if val, exists := updateMap["size"]; exists {
		if size, ok := val.(float64); ok {
			torrent.Size = int64(size)
		}
	}
	if val, exists := updateMap["completed"]; exists {
		if completed, ok := val.(float64); ok {
			torrent.Completed = int64(completed)
		}
	}
	if val, exists := updateMap["ratio"]; exists {
		if ratio, ok := val.(float64); ok {
			torrent.Ratio = ratio
		}
	}
	if val, exists := updateMap["priority"]; exists {
		if priority, ok := val.(float64); ok {
			torrent.Priority = int64(priority)
		}
	}
	if val, exists := updateMap["num_seeds"]; exists {
		if seeds, ok := val.(float64); ok {
			torrent.NumSeeds = int64(seeds)
		}
	}
	if val, exists := updateMap["num_leechs"]; exists {
		if leechs, ok := val.(float64); ok {
			torrent.NumLeechs = int64(leechs)
		}
	}
	if val, exists := updateMap["eta"]; exists {
		if eta, ok := val.(float64); ok {
			torrent.ETA = int64(eta)
		}
	}
	if val, exists := updateMap["seq_dl"]; exists {
		if seqDl, ok := val.(bool); ok {
			torrent.SequentialDownload = seqDl
		}
	}
	if val, exists := updateMap["f_l_piece_prio"]; exists {
		if flPiecePrio, ok := val.(bool); ok {
			torrent.FirstLastPiecePrio = flPiecePrio
		}
	}
	if val, exists := updateMap["force_start"]; exists {
		if forceStart, ok := val.(bool); ok {
			torrent.ForceStart = forceStart
		}
	}
	if val, exists := updateMap["super_seeding"]; exists {
		if superSeeding, ok := val.(bool); ok {
			torrent.SuperSeeding = superSeeding
		}
	}
	if val, exists := updateMap["added_on"]; exists {
		if addedOn, ok := val.(float64); ok {
			torrent.AddedOn = int64(addedOn)
		}
	}
	if val, exists := updateMap["completion_on"]; exists {
		if completionOn, ok := val.(float64); ok {
			torrent.CompletionOn = int64(completionOn)
		}
	}
	if val, exists := updateMap["tracker"]; exists {
		if tracker, ok := val.(string); ok {
			torrent.Tracker = tracker
		}
	}
	if val, exists := updateMap["save_path"]; exists {
		if savePath, ok := val.(string); ok {
			torrent.SavePath = savePath
		}
	}
	if val, exists := updateMap["content_path"]; exists {
		if contentPath, ok := val.(string); ok {
			torrent.ContentPath = contentPath
		}
	}
	if val, exists := updateMap["seeding_time"]; exists {
		if seedingTime, ok := val.(float64); ok {
			torrent.SeedingTime = int64(seedingTime)
		}
	}
	if val, exists := updateMap["time_active"]; exists {
		if timeActive, ok := val.(float64); ok {
			torrent.TimeActive = int64(timeActive)
		}
	}
}

// ensureFreshData checks if data is stale or missing and syncs if needed
func (sm *SyncManager) ensureFreshData() {
	sm.mu.RLock()

	// Check if data is stale or nil and we should sync
	shouldSync := false
	if sm.data == nil {
		// If data is nil, only sync if DynamicSync is enabled
		shouldSync = sm.options.DynamicSync
	} else if sm.options.DynamicSync {
		// Only check staleness if DynamicSync is enabled
		staleThreshold := sm.calculateStaleThreshold()
		if time.Since(sm.lastSync) > staleThreshold {
			shouldSync = true
		}
	}
	sm.mu.RUnlock()

	// Perform sync if data is stale or nil
	if shouldSync {
		_ = sm.Sync(context.Background()) // Use background context, ignore error
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

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.data == nil {
		return nil
	}

	result := make([]Torrent, len(sm.data.Torrents))
	count := 0
	for _, torrent := range sm.data.Torrents {
		if sm.matchesFilter(torrent, options) {
			result[count] = torrent
			count++
		}
	}
	filtered := result[:count]

	filtered = sm.processFilteredTorrents(filtered, options)

	return filtered
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

func (sm *SyncManager) matchesFilter(torrent Torrent, options TorrentFilterOptions) bool {
	if len(options.Hashes) > 0 {
		found := false
		for _, h := range options.Hashes {
			if h == torrent.Hash {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if options.Category != "" && torrent.Category != options.Category {
		return false
	}
	if options.Tag != "" && !strings.Contains(torrent.Tags, options.Tag) {
		return false
	}
	if options.Filter != "" && !sm.matchesStateFilter(torrent.State, options.Filter) {
		return false
	}
	return true
}

func (sm *SyncManager) matchesStateFilter(state TorrentState, filter TorrentFilter) bool {
	switch filter {
	case TorrentFilterAll:
		return true
	case TorrentFilterDownloading:
		return state == TorrentStateDownloading || state == TorrentStateMetaDl || state == TorrentStateStalledDl || state == TorrentStateCheckingDl || state == TorrentStateForcedDl || state == TorrentStateAllocating
	case TorrentFilterUploading:
		return state == TorrentStateUploading || state == TorrentStateStalledUp || state == TorrentStateCheckingUp || state == TorrentStateForcedUp
	case TorrentFilterCompleted:
		return state == TorrentStatePausedUp || state == TorrentStateStoppedUp || state == TorrentStateQueuedUp || state == TorrentStateStalledUp || state == TorrentStateCheckingUp || state == TorrentStateForcedUp
	case TorrentFilterPaused:
		return state == TorrentStatePausedDl || state == TorrentStatePausedUp
	case TorrentFilterActive:
		return state == TorrentStateDownloading || state == TorrentStateUploading || state == TorrentStateMetaDl || state == TorrentStateStalledDl || state == TorrentStateStalledUp || state == TorrentStateCheckingDl || state == TorrentStateCheckingUp || state == TorrentStateForcedDl || state == TorrentStateForcedUp || state == TorrentStateAllocating
	case TorrentFilterInactive:
		return state == TorrentStatePausedDl || state == TorrentStatePausedUp || state == TorrentStateStoppedDl || state == TorrentStateStoppedUp || state == TorrentStateQueuedDl || state == TorrentStateQueuedUp || state == TorrentStateStalledDl || state == TorrentStateStalledUp
	case TorrentFilterResumed:
		return state == TorrentStateDownloading || state == TorrentStateUploading || state == TorrentStateMetaDl || state == TorrentStateStalledDl || state == TorrentStateStalledUp || state == TorrentStateCheckingDl || state == TorrentStateCheckingUp || state == TorrentStateForcedDl || state == TorrentStateForcedUp || state == TorrentStateAllocating
	case TorrentFilterStopped:
		return state == TorrentStateStoppedDl || state == TorrentStateStoppedUp
	case TorrentFilterStalled:
		return state == TorrentStateStalledDl || state == TorrentStateStalledUp
	case TorrentFilterStalledDownloading:
		return state == TorrentStateStalledDl
	case TorrentFilterStalledUploading:
		return state == TorrentStateStalledUp
	default:
		return true
	}
}

// processFilteredTorrents applies sorting, reverse, limit, and offset to the filtered torrents
func (sm *SyncManager) processFilteredTorrents(filtered []Torrent, options TorrentFilterOptions) []Torrent {
	// Sort
	if options.Sort != "" {
		sort.Slice(filtered, func(i, j int) bool {
			var less bool
			switch options.Sort {
			case "name":
				less = filtered[i].Name < filtered[j].Name
			case "size":
				less = filtered[i].Size < filtered[j].Size
			case "progress":
				less = filtered[i].Progress < filtered[j].Progress
			case "added_on":
				less = filtered[i].AddedOn < filtered[j].AddedOn
			case "state":
				less = string(filtered[i].State) < string(filtered[j].State)
			default:
				less = filtered[i].Name < filtered[j].Name // default to name
			}
			if options.Reverse {
				return !less
			}
			return less
		})
	}

	// Apply offset and limit
	if options.Offset > 0 || options.Limit > 0 {
		start := options.Offset
		if start >= len(filtered) {
			filtered = filtered[:0]
		} else {
			end := len(filtered)
			if options.Limit > 0 && start+options.Limit < end {
				end = start + options.Limit
			}
			filtered = filtered[start:end]
		}
	}

	return filtered
}

// GetTorrent returns a specific torrent by hash
func (sm *SyncManager) GetTorrent(hash string) (Torrent, bool) {
	sm.ensureFreshData()

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.data == nil {
		return Torrent{}, false
	}

	torrent, exists := sm.data.Torrents[hash]
	return torrent, exists
}

// GetServerState returns the current server state
// GetServerState returns the current server state
func (sm *SyncManager) GetServerState() ServerState {
	sm.ensureFreshData()

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.data == nil {
		return ServerState{}
	}

	return sm.data.ServerState
}

// GetCategories returns a copy of all categories
// GetCategories returns a copy of all categories
func (sm *SyncManager) GetCategories() map[string]Category {
	sm.ensureFreshData()

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
// GetTags returns a copy of all tags
func (sm *SyncManager) GetTags() []string {
	sm.ensureFreshData()

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

// removeDuplicateStrings removes duplicate strings from a slice
func removeDuplicateStrings(input []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(input))

	for _, item := range input {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}

	return result
}

// removeStrings removes specified strings from a slice
func removeStrings(input []string, toRemove []string) []string {
	removeMap := make(map[string]bool)
	for _, item := range toRemove {
		removeMap[item] = true
	}

	result := make([]string, 0, len(input))
	for _, item := range input {
		if !removeMap[item] {
			result = append(result, item)
		}
	}

	return result
}
