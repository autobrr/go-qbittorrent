//go:generate go run internal/codegen/generate_maindata_updaters.go

package qbittorrent

import (
	"context"

	"golang.org/x/exp/slices"
)

// normalizeHash sets .Hash from InfohashV1 or InfohashV2 if Hash is empty
func normalizeHash(torrent *Torrent) {
	if torrent.Hash == "" {
		if torrent.InfohashV1 != "" {
			torrent.Hash = torrent.InfohashV1
		} else if torrent.InfohashV2 != "" {
			torrent.Hash = torrent.InfohashV2
		}
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
	
	// Normalize hashes for all torrents after full update
	for hash, torrent := range dest.Torrents {
		normalizeHash(&torrent)
		dest.Torrents[hash] = torrent
	}
	
	return nil
}

func merge[T map[string]V, V any](s T, d *T) {
	for k, v := range s {
		(*d)[k] = v
	}
}

func remove[T map[string]V, V any](s []string, d *T) {
	for _, v := range s {
		delete(*d, v)
	}
}

func mergeSlice[T []string](s T, d *T) {
	*d = append(*d, s...)
	slices.Sort(*d)
	*d = slices.Compact(*d)
}

func removeSlice[T []string](s T, d *T) {
	for i := 0; i < len(*d); i++ {
		if k := (*d)[i]; len(k) != 0 {
			match := false
			for _, c := range s {
				if c == k {
					match = true
					break
				}
			}

			if !match {
				continue
			}
		}

		(*d)[i] = (*d)[len(*d)-1]
		(*d) = (*d)[:len(*d)-1]
		i--
	}
}

// UpdateWithRawData efficiently merges partial updates using raw JSON data
// This provides field-level merging similar to the SyncManager's logic
func (dest *MainData) UpdateWithRawData(rawData map[string]interface{}, source *MainData) {
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
			dest.updateServerStateFields(&dest.ServerState, serverStateMap)
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
		dest.updateTorrentFields(&existing, updateMap)
		
		// Normalize hash: set .Hash from InfohashV1 or InfohashV2 if Hash is empty
		normalizeHash(&existing)
		
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
		dest.updateCategoryFields(&existing, updateMap)
		dest.Categories[name] = existing
	}
}
