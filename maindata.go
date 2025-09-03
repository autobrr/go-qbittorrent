package qbittorrent

import (
	"context"

	"golang.org/x/exp/slices"
)

func (dest *MainData) Update(ctx context.Context, c *Client) error {
	source, err := c.SyncMainDataCtx(ctx, int64(dest.Rid))
	if err != nil {
		return err
	}

	if source.FullUpdate {
		*dest = *source
		return nil
	}

	dest.Rid = source.Rid
	merge(source.Categories, &dest.Categories)
	merge(source.Torrents, &dest.Torrents)
	merge(source.Trackers, &dest.Trackers)
	mergeServerState(source.ServerState, &dest.ServerState)
	remove(source.CategoriesRemoved, &dest.Categories)
	remove(source.TorrentsRemoved, &dest.Torrents)
	mergeSlice(source.Tags, &dest.Tags)
	removeSlice(source.TagsRemoved, &dest.Tags)
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
