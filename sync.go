package qbittorrent

import (
	"context"
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
		sm.syncMu.Unlock()

		// Now safely read the cached error from the completed sync
		sm.mu.RLock()
		cachedError := sm.lastError
		sm.mu.RUnlock()
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

	// Initialize data if needed
	if sm.data == nil {
		sm.data = &MainData{
			Torrents:   make(map[string]Torrent),
			Categories: make(map[string]Category),
			Trackers:   make(map[string][]string),
			Tags:       make([]string, 0),
		}
	}

	// Use MainData.Update to handle all the sync logic
	if err := sm.data.Update(ctx, sm.client); err != nil {
		syncError = err
		if sm.options.OnError != nil {
			sm.options.OnError(err)
		}
		return err
	}

	sm.rid = sm.data.Rid

	// Call update callback if set
	if sm.options.OnUpdate != nil {
		sm.options.OnUpdate(sm.copyMainData(sm.data))
	}

	// Success - clear any previous error
	syncError = nil
	return nil
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

	result := make([]Torrent, 0, len(sm.data.Torrents))
	for _, torrent := range sm.data.Torrents {
		if matchesTorrentFilter(torrent, options) {
			result = append(result, torrent)
		}
	}

	return applyTorrentFilterOptions(result, options)
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

// PeerSyncManager manages synchronization of peer data for a specific torrent
// It handles incremental updates efficiently using the rid parameter
type PeerSyncManager struct {
	client   *Client
	mu       sync.RWMutex
	hash     string
	data     *TorrentPeersResponse
	lastSync time.Time
	options  PeerSyncOptions
}

// PeerSyncOptions configures the behavior of the peer sync manager
type PeerSyncOptions struct {
	// AutoSync enables automatic periodic syncing
	AutoSync bool
	// SyncInterval is the interval between automatic syncs (default: 5s)
	SyncInterval time.Duration
	// OnUpdate is called whenever the peer data is updated
	OnUpdate func(*TorrentPeersResponse)
	// OnError is called when sync encounters an error
	OnError func(error)
}

// DefaultPeerSyncOptions returns sensible default options
func DefaultPeerSyncOptions() PeerSyncOptions {
	return PeerSyncOptions{
		AutoSync:     false,
		SyncInterval: 5 * time.Second,
	}
}

// NewPeerSyncManager creates a new peer sync manager for a specific torrent
func NewPeerSyncManager(client *Client, hash string, options ...PeerSyncOptions) *PeerSyncManager {
	opts := DefaultPeerSyncOptions()
	if len(options) > 0 {
		opts = options[0]
	}

	if opts.SyncInterval == 0 {
		opts.SyncInterval = 5 * time.Second
	}

	return &PeerSyncManager{
		client:  client,
		hash:    hash,
		options: opts,
		data: &TorrentPeersResponse{
			Peers: make(map[string]TorrentPeer),
			Rid:   0,
		},
	}
}

// Start initializes the peer sync manager and optionally starts auto-sync
func (psm *PeerSyncManager) Start(ctx context.Context) error {
	// Perform initial full sync
	if err := psm.Sync(ctx); err != nil {
		return err
	}

	// Start auto-sync if enabled
	if psm.options.AutoSync {
		go psm.autoSync(ctx)
	}

	return nil
}

// Sync performs a synchronization with the qBittorrent server
func (psm *PeerSyncManager) Sync(ctx context.Context) error {
	psm.mu.Lock()
	rid := psm.data.Rid
	psm.mu.Unlock()

	// Get peer update from server
	update, err := psm.client.GetTorrentPeersCtx(ctx, psm.hash, rid)
	if err != nil {
		if psm.options.OnError != nil {
			psm.options.OnError(err)
		}
		return err
	}

	psm.mu.Lock()
	// Merge the update into our existing data
	psm.data.MergePeers(update)
	psm.lastSync = time.Now()
	psm.mu.Unlock()

	// Call update callback if set
	if psm.options.OnUpdate != nil {
		psm.options.OnUpdate(psm.GetPeers())
	}

	return nil
}

// GetPeers returns a copy of the current peer data
func (psm *PeerSyncManager) GetPeers() *TorrentPeersResponse {
	psm.mu.RLock()
	defer psm.mu.RUnlock()

	// Create a deep copy
	peers := make(map[string]TorrentPeer, len(psm.data.Peers))
	for k, v := range psm.data.Peers {
		peers[k] = v
	}

	removed := make([]string, len(psm.data.PeersRemoved))
	copy(removed, psm.data.PeersRemoved)

	return &TorrentPeersResponse{
		Peers:        peers,
		PeersRemoved: removed,
		Rid:          psm.data.Rid,
		FullUpdate:   psm.data.FullUpdate,
		ShowFlags:    psm.data.ShowFlags,
	}
}

// GetPeerCount returns the current number of connected peers
func (psm *PeerSyncManager) GetPeerCount() int {
	psm.mu.RLock()
	defer psm.mu.RUnlock()
	return len(psm.data.Peers)
}

// autoSync runs periodic synchronization in the background
func (psm *PeerSyncManager) autoSync(ctx context.Context) {
	ticker := time.NewTicker(psm.options.SyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			_ = psm.Sync(ctx) // Ignore errors in auto-sync
		case <-ctx.Done():
			return
		}
	}
}

// Stop stops auto-sync if it's running
func (psm *PeerSyncManager) Stop() {
	// Auto-sync will stop when context is cancelled
	// This is handled by the caller cancelling the context passed to Start
}

// MergePeers merges a partial peer update into an existing peer list
// This handles incremental updates from the sync/torrentPeers endpoint
func (r *TorrentPeersResponse) MergePeers(update *TorrentPeersResponse) {
	// If it's a full update, replace everything
	if update.FullUpdate {
		r.Peers = update.Peers
		r.PeersRemoved = nil
		r.Rid = update.Rid
		r.ShowFlags = update.ShowFlags
		return
	}

	// Initialize peers map if nil
	if r.Peers == nil {
		r.Peers = make(map[string]TorrentPeer)
	}

	// Apply partial update
	for peerKey, updatePeer := range update.Peers {
		if existingPeer, exists := r.Peers[peerKey]; exists {
			// Merge fields - only update non-zero/non-empty fields
			r.Peers[peerKey] = mergePeerFields(existingPeer, updatePeer)
		} else {
			// New peer - add it
			r.Peers[peerKey] = updatePeer
		}
	}

	// Remove peers that are no longer present
	for _, peerKey := range update.PeersRemoved {
		delete(r.Peers, peerKey)
	}

	// Update response ID
	r.Rid = update.Rid
	if update.ShowFlags {
		r.ShowFlags = update.ShowFlags
	}
}

// mergePeerFields merges update fields into existing peer, preserving non-updated fields
func mergePeerFields(existing, update TorrentPeer) TorrentPeer {
	// Start with existing peer data
	result := existing

	// Only update fields that are present in the update (non-zero/non-empty)
	if update.IP != "" {
		result.IP = update.IP
	}
	if update.Connection != "" {
		result.Connection = update.Connection
	}
	if update.Flags != "" {
		result.Flags = update.Flags
	}
	if update.FlagsDesc != "" {
		result.FlagsDesc = update.FlagsDesc
	}
	if update.Client != "" {
		result.Client = update.Client
	}
	if update.Files != "" {
		result.Files = update.Files
	}
	if update.Country != "" {
		result.Country = update.Country
	}
	if update.CountryCode != "" {
		result.CountryCode = update.CountryCode
	}
	if update.PeerIDClient != "" {
		result.PeerIDClient = update.PeerIDClient
	}
	if update.Port != 0 {
		result.Port = update.Port
	}

	// Update numeric fields that can change frequently
	// Progress, speeds, and relevance should always be updated
	result.Progress = update.Progress
	result.DownSpeed = update.DownSpeed
	result.UpSpeed = update.UpSpeed
	result.Relevance = update.Relevance

	// For Downloaded and Uploaded, only update if non-zero
	// These are cumulative values that shouldn't be reset to 0 in partial updates
	if update.Downloaded != 0 {
		result.Downloaded = update.Downloaded
	}
	if update.Uploaded != 0 {
		result.Uploaded = update.Uploaded
	}

	return result
}
