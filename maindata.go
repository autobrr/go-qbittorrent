//go:generate go run internal/codegen/generate_maindata_updaters.go

package qbittorrent

import (
	"context"
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
	source, rawData, err := c.SyncMainDataCtxWithRaw(ctx, int64(dest.Rid))
	if err != nil {
		return err
	}

	// If this is a partial update (FullUpdate is false), use UpdateWithRawData
	if !source.FullUpdate {
		dest.UpdateWithRawData(rawData, source)
		return nil
	}

	// For full updates, replace everything
	*dest = *source
	dest.ensureInitialized()
	return nil
}

// UpdateWithRawData efficiently merges partial updates using raw JSON data
// This provides field-level merging similar to the SyncManager's logic
func (dest *MainData) UpdateWithRawData(rawData map[string]interface{}, source *MainData) {
	dest.ensureInitialized()

	// Update RID
	dest.Rid = source.Rid

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
	}

	// Handle categories ONLY if present in raw JSON
	if categoriesRaw, exists := rawData["categories"]; exists {
		if categoriesMap, ok := categoriesRaw.(map[string]interface{}); ok {
			dest.mergeCategoriesPartial(categoriesMap)
		}
	}
	if len(source.CategoriesRemoved) > 0 {
		remove(source.CategoriesRemoved, &dest.Categories)
	}

	// Handle trackers ONLY if present in raw JSON
	if trackersRaw, exists := rawData["trackers"]; exists {
		if _, ok := trackersRaw.(map[string]interface{}); ok {
			merge(source.Trackers, &dest.Trackers)
		}
	}

	// Handle tags ONLY if present in raw JSON
	if tagsRaw, exists := rawData["tags"]; exists {
		if _, ok := tagsRaw.([]interface{}); ok {
			mergeSlice(source.Tags, &dest.Tags)
		}
	}
	if len(source.TagsRemoved) > 0 {
		removeSlice(source.TagsRemoved, &dest.Tags)
	}

	// Handle server_state partial updates ONLY if present in raw JSON
	if serverStateRaw, exists := rawData["server_state"]; exists {
		if serverStateMap, ok := serverStateRaw.(map[string]interface{}); ok {
			updateServerStateFields(&dest.ServerState, serverStateMap)
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
