package qbittorrent

import (
	"context"
	"sync"
	"time"
)

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
	AutoSync     bool
	SyncInterval time.Duration
	OnUpdate     func(*TorrentPeersResponse)
	OnError      func(error)
}

// DefaultPeerSyncOptions returns the default options for peer sync
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

	// Apply update
	psm.mu.Lock()
	psm.data.MergePeers(update)
	psm.lastSync = time.Now()
	psm.mu.Unlock()

	// Notify callback if configured
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

	// Apply partial update using generic merge
	for peerKey, updatePeer := range update.Peers {
		if existingPeer, exists := r.Peers[peerKey]; exists {
			// Merge fields - only update non-zero/non-empty fields
			r.Peers[peerKey] = mergePeerFields(existingPeer, updatePeer)
		} else {
			// New peer - add it
			r.Peers[peerKey] = updatePeer
		}
	}

	// Remove peers using the generic remove function from maindata.go
	remove(update.PeersRemoved, &r.Peers)

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

