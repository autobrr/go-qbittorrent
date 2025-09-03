//go:generate go run internal/codegen/generate_maindata_updaters.go

package qbittorrent

import (
	"context"
	"encoding/json"
	"fmt"

	"golang.org/x/exp/slices"
)

func (dest *MainData) Update(ctx context.Context, c *Client) error {
	// Get raw JSON data to know which fields are actually present
	resp, err := c.getCtx(ctx, "/sync/maindata", map[string]string{
		"rid": fmt.Sprintf("%d", dest.Rid),
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var rawData map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&rawData); err != nil {
		return err
	}

	// Also parse into MainData struct for convenience
	source, err := c.SyncMainDataCtx(ctx, int64(dest.Rid))
	if err != nil {
		return err
	}

	if source.FullUpdate {
		*dest = *source
		return nil
	}

	// Use the sophisticated field-level merging
	dest.UpdateWithRawData(rawData, source)
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

func mergeServerState(source ServerState, dest *ServerState) {
	if source.AlltimeDl > 0 {
		dest.AlltimeDl = source.AlltimeDl
	}
	if source.AlltimeUl > 0 {
		dest.AlltimeUl = source.AlltimeUl
	}
	if source.AverageTimeQueue > 0 {
		dest.AverageTimeQueue = source.AverageTimeQueue
	}
	if source.ConnectionStatus != "" {
		dest.ConnectionStatus = source.ConnectionStatus
	}
	if source.DhtNodes > 0 {
		dest.DhtNodes = source.DhtNodes
	}
	if source.DlInfoData > 0 {
		dest.DlInfoData = source.DlInfoData
	}
	if source.DlInfoSpeed >= 0 {
		dest.DlInfoSpeed = source.DlInfoSpeed
	}
	if source.DlRateLimit >= 0 {
		dest.DlRateLimit = source.DlRateLimit
	}
	if source.FreeSpaceOnDisk > 0 {
		dest.FreeSpaceOnDisk = source.FreeSpaceOnDisk
	}
	if source.GlobalRatio != "" {
		dest.GlobalRatio = source.GlobalRatio
	}
	if source.QueuedIoJobs >= 0 {
		dest.QueuedIoJobs = source.QueuedIoJobs
	}
	dest.Queueing = source.Queueing
	if source.ReadCacheHits != "" {
		dest.ReadCacheHits = source.ReadCacheHits
	}
	if source.ReadCacheOverload != "" {
		dest.ReadCacheOverload = source.ReadCacheOverload
	}
	if source.RefreshInterval > 0 {
		dest.RefreshInterval = source.RefreshInterval
	}
	if source.TotalBuffersSize >= 0 {
		dest.TotalBuffersSize = source.TotalBuffersSize
	}
	if source.TotalPeerConnections >= 0 {
		dest.TotalPeerConnections = source.TotalPeerConnections
	}
	if source.TotalQueuedSize >= 0 {
		dest.TotalQueuedSize = source.TotalQueuedSize
	}
	if source.TotalWastedSession >= 0 {
		dest.TotalWastedSession = source.TotalWastedSession
	}
	if source.UpInfoData > 0 {
		dest.UpInfoData = source.UpInfoData
	}
	if source.UpInfoSpeed >= 0 {
		dest.UpInfoSpeed = source.UpInfoSpeed
	}
	if source.UpRateLimit >= 0 {
		dest.UpRateLimit = source.UpRateLimit
	}
	dest.UseAltSpeedLimits = source.UseAltSpeedLimits
	if source.WriteCacheOverload != "" {
		dest.WriteCacheOverload = source.WriteCacheOverload
	}
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
	// Update RID and server state
	dest.Rid = source.Rid
	dest.ServerState = source.ServerState

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
