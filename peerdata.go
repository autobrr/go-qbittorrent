package qbittorrent

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

	// Remove peers using the generic remove function from helpers.go
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
	// Speeds should always be updated (they can legitimately be 0)
	result.DownSpeed = update.DownSpeed
	result.UpSpeed = update.UpSpeed

	// Progress should only be updated if it's explicitly provided in the partial update
	// A seeder with progress=1.0 might receive partial updates with progress=0 when nothing changed
	// We need to check if progress was actually included in the update
	// Since we removed omitempty, we need a different approach to detect if it was included
	// For now, only update progress if it's different from 0 OR if it's explicitly being set to 0
	// from a non-zero value (which would mean the peer went backwards)
	if update.Progress > 0 || (update.Progress == 0 && existing.Progress > 0 && existing.Progress < 1.0) {
		result.Progress = update.Progress
	}

	// Relevance should always be updated
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
