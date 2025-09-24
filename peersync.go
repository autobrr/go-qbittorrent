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
