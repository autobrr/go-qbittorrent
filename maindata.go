//go:generate go run internal/codegen/generate_maindata_updaters.go

package qbittorrent

import (
	"context"
	"maps"
	"slices"
)

// normalizeHash sets .Hash from InfohashV1 or InfohashV2 if Hash is empty
func normalizeHashes(dest map[string]Torrent) {
	// Normalize hashes for all torrents after full update
	for hash, torrent := range dest {
		torrent.Hash = hash
		dest[hash] = torrent
	}
}

// normalizeHashesRaw normalizes hashes in raw JSON data format
func normalizeHashesRaw(rawData map[string]interface{}) {
	if torrentsRaw, exists := rawData["torrents"]; exists {
		if torrentsMap, ok := torrentsRaw.(map[string]interface{}); ok {
			for hash, torrentRaw := range torrentsMap {
				if torrentMap, ok := torrentRaw.(map[string]interface{}); ok {
					torrentMap["hash"] = hash
				}
			}
		}
	}
}

// ensureInitialized prepares MainData maps/slices so merge helpers can write safely.
func (dest *MainData) ensureInitialized() {
	if dest.Torrents == nil {
		dest.Torrents = make(map[string]Torrent)
	}
	if dest.Categories == nil {
		dest.Categories = make(map[string]Category)
	}
	if dest.Trackers == nil {
		dest.Trackers = make(map[string][]string)
	}
	if dest.Tags == nil {
		dest.Tags = make([]string, 0)
	}
}

func (dest *MainData) Update(ctx context.Context, c *Client) error {
	source, rawData, err := c.SyncMainDataCtxWithRaw(ctx, dest.Rid)
	if err != nil {
		// Ask for a full update next time. SyncManager.applySyncResult tells why.
		dest.Rid = 0
		return err
	}

	dest.applySync(source, rawData)
	return nil
}

// applySync merges an already-fetched maindata response into dest. It is
// Update without the network call, so callers can perform the fetch without
// holding whatever lock guards dest.
func (dest *MainData) applySync(source *MainData, rawData map[string]interface{}) {
	// If this is a partial update (FullUpdate is false), use UpdateWithRawData
	if !source.FullUpdate {
		dest.UpdateWithRawData(rawData, source)
		return
	}

	// For full updates, replace everything
	old := dest.Torrents
	*dest = *source
	dest.ensureInitialized()

	dest.Changes = MainDataChanges{
		Torrents:    slices.Collect(maps.Keys(dest.Torrents)),
		Categories:  true,
		Tags:        true,
		Trackers:    true,
		ServerState: true,
	}
	for hash := range old {
		if _, ok := dest.Torrents[hash]; !ok {
			dest.Changes.TorrentsRemoved = append(dest.Changes.TorrentsRemoved, hash)
		}
	}
}

// UpdateWithRawData efficiently merges partial updates using raw JSON data
// This provides field-level merging similar to the SyncManager's logic
func (dest *MainData) UpdateWithRawData(rawData map[string]interface{}, source *MainData) {
	dest.ensureInitialized()

	dest.Rid = source.Rid
	dest.FullUpdate = source.FullUpdate
	dest.Changes = MainDataChanges{}

	// Handle torrents ONLY if the torrents field is present in the raw JSON
	// This prevents clearing torrents when there's no torrent update
	if torrentsRaw, exists := rawData["torrents"]; exists {
		if torrentsMap, ok := torrentsRaw.(map[string]interface{}); ok {
			dest.mergeTorrentsPartial(torrentsMap)
		}
	}

	// Remove deleted torrents ONLY if there are actually items to remove
	if len(source.TorrentsRemoved) > 0 {
		remove(source.TorrentsRemoved, &dest.Torrents)
		dest.removeTrackerHashes(source.TorrentsRemoved)
		dest.Changes.TorrentsRemoved = source.TorrentsRemoved
	}

	// Handle categories ONLY if present in raw JSON
	if categoriesRaw, exists := rawData["categories"]; exists {
		if categoriesMap, ok := categoriesRaw.(map[string]interface{}); ok {
			dest.mergeCategoriesPartial(categoriesMap)
			dest.Changes.Categories = len(categoriesMap) > 0
		}
	}
	if len(source.CategoriesRemoved) > 0 {
		remove(source.CategoriesRemoved, &dest.Categories)
		dest.Changes.Categories = true
	}

	// Handle trackers ONLY if present in raw JSON
	if trackersRaw, exists := rawData["trackers"]; exists {
		if trackersMap, ok := trackersRaw.(map[string]interface{}); ok {
			merge(source.Trackers, &dest.Trackers)
			dest.Changes.Trackers = dest.Changes.Trackers || len(trackersMap) > 0
		}
	}
	if len(source.TrackersRemoved) > 0 {
		remove(source.TrackersRemoved, &dest.Trackers)
		dest.Changes.Trackers = true
	}

	// Handle tags ONLY if present in raw JSON
	if tagsRaw, exists := rawData["tags"]; exists {
		if tagsList, ok := tagsRaw.([]interface{}); ok {
			mergeSlice(source.Tags, &dest.Tags)
			dest.Changes.Tags = len(tagsList) > 0
		}
	}
	if len(source.TagsRemoved) > 0 {
		removeSlice(source.TagsRemoved, &dest.Tags)
		dest.Changes.Tags = true
	}

	// Handle server_state partial updates ONLY if present in raw JSON
	if serverStateRaw, exists := rawData["server_state"]; exists {
		if serverStateMap, ok := serverStateRaw.(map[string]interface{}); ok {
			updateServerStateFields(&dest.ServerState, serverStateMap)
			dest.Changes.ServerState = len(serverStateMap) > 0
		}
	}
}

// mergeTorrentsPartial merges only the fields that are present in the update
func (dest *MainData) mergeTorrentsPartial(torrentsMap map[string]interface{}) {
	for hash, torrentRaw := range torrentsMap {
		updateMap, ok := torrentRaw.(map[string]interface{})
		if !ok {
			continue
		}

		existing, exists := dest.Torrents[hash]
		if !exists {
			// New torrent - create a minimal torrent with the hash at least
			existing = Torrent{Hash: hash}
		}

		// Always start with existing data and update only provided fields
		updateTorrentFields(&existing, updateMap)

		dest.Torrents[hash] = existing
		dest.Changes.Torrents = append(dest.Changes.Torrents, hash)
	}
}

// removeTrackerHashes removes the given torrent hashes from each tracker,
// and removes each tracker that has no torrent left.
func (dest *MainData) removeTrackerHashes(hashes []string) {
	removed := make(map[string]struct{}, len(hashes))
	for _, hash := range hashes {
		removed[hash] = struct{}{}
	}
	isRemoved := func(hash string) bool {
		_, ok := removed[hash]
		return ok
	}

	for url, trackerHashes := range dest.Trackers {
		if !slices.ContainsFunc(trackerHashes, isRemoved) {
			continue
		}
		// Change a copy: GetTrackers and OnUpdate share the old slice with callers.
		kept := slices.DeleteFunc(slices.Clone(trackerHashes), isRemoved)
		if len(kept) == 0 {
			delete(dest.Trackers, url)
		} else {
			dest.Trackers[url] = kept
		}
		dest.Changes.Trackers = true
	}
}

// mergeCategoriesPartial merges only the fields that are present in the update
func (dest *MainData) mergeCategoriesPartial(categoriesMap map[string]interface{}) {
	for name, categoryRaw := range categoriesMap {
		updateMap, ok := categoryRaw.(map[string]interface{})
		if !ok {
			continue
		}

		existing, exists := dest.Categories[name]
		if !exists {
			// New category - create a minimal category with the name at least
			existing = Category{Name: name}
		}

		// Always start with existing data and update only provided fields
		updateCategoryFields(&existing, updateMap)
		dest.Categories[name] = existing
	}
}
