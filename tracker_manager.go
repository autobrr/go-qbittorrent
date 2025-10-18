package qbittorrent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Masterminds/semver"
	"github.com/autobrr/autobrr/pkg/ttlcache"
)

const (
	trackerCacheTTL = 30 * time.Minute
)

// trackerAPI describes the subset of Client functionality required by TrackerManager.
type trackerAPI interface {
	GetTorrentsCtx(ctx context.Context, o TorrentFilterOptions) ([]Torrent, error)
	getApiVersion() (*semver.Version, error)
	GetTorrentTrackersCtx(ctx context.Context, hash string) ([]TorrentTracker, error)
}

// TrackerManager coordinates tracker metadata hydration with caching.
type TrackerManager struct {
	api   trackerAPI
	cache *ttlcache.Cache[string, []TorrentTracker]
}

// NewTrackerManager constructs a manager for tracker metadata caching.
func NewTrackerManager(api trackerAPI) *TrackerManager {
	manager := &TrackerManager{
		api:   api,
		cache: ttlcache.New(ttlcache.Options[string, []TorrentTracker]{}.SetDefaultTTL(trackerCacheTTL)),
	}

	return manager
}

// HydrateTorrents enriches the provided torrents with tracker metadata from cache.
// For versions that don't support trackers in sync, fetches individually if not cached.
// It returns the enriched slice and a cache of tracker lists keyed by hash.
func (tm *TrackerManager) HydrateTorrents(ctx context.Context, torrents []Torrent) ([]Torrent, map[string][]TorrentTracker) {
	if tm == nil || len(torrents) == 0 {
		return torrents, nil
	}

	trackerMap := make(map[string][]TorrentTracker, len(torrents))

	for i := range torrents {
		hash := strings.TrimSpace(torrents[i].Hash)
		if hash == "" {
			continue
		}

		if len(torrents[i].Trackers) > 0 {
			trackerMap[hash] = torrents[i].Trackers
			continue
		}

		if trackers, ok := tm.cache.Get(hash); ok {
			torrents[i].Trackers = trackers
			trackerMap[hash] = trackers
			continue
		}

		// For versions that don't support trackers in sync, fetch individually
		if trackers, err := tm.fetchTrackersForHash(ctx, hash); err == nil && len(trackers) > 0 {
			torrents[i].Trackers = trackers
			trackerMap[hash] = trackers
			// Cache with default TTL since we don't have Reannounce time
			tm.cache.Set(hash, trackers, trackerCacheTTL)
		}
	}

	return torrents, trackerMap
}

// Invalidate clears cached tracker metadata for the supplied hashes. When no hashes are provided
// the entire cache is purged.
func (tm *TrackerManager) Invalidate(hashes ...string) {
	if tm == nil || tm.cache == nil {
		return
	}

	if len(hashes) == 0 {
		for _, key := range tm.cache.GetKeys() {
			if key == "" {
				continue
			}
			tm.cache.Delete(key)
		}
		return
	}

	for _, hash := range hashes {
		hash = strings.TrimSpace(hash)
		if hash == "" {
			continue
		}
		tm.cache.Delete(hash)
	}
}

// UpdateFromSync updates the tracker cache with data from a sync operation.
// It uses the torrent's Reannounce time to determine cache TTL.
func (tm *TrackerManager) UpdateFromSync(data *MainData) {
	if tm == nil || tm.cache == nil || data == nil || len(data.Torrents) == 0 {
		return
	}

	now := time.Now()
	for hash, torrent := range data.Torrents {
		if len(torrent.Trackers) == 0 {
			continue
		}

		// Calculate TTL based on Reannounce time
		ttl := trackerCacheTTL
		if torrent.Reannounce > 0 {
			reannounceTime := time.Unix(torrent.Reannounce, 0)
			if reannounceTime.After(now) {
				ttl = reannounceTime.Sub(now)
				// Cap at maximum TTL to prevent extremely long cache times
				if ttl > trackerCacheTTL {
					ttl = trackerCacheTTL
				}
			}
		}

		tm.cache.Set(hash, torrent.Trackers, ttl)
	}
}

func (tm *TrackerManager) detectIncludeTrackers() bool {
	if tm == nil || tm.api == nil {
		return false
	}

	ver, err := tm.api.getApiVersion()
	if err != nil || ver == nil {
		return false
	}

	required := semver.MustParse("2.11.4")
	return !ver.LessThan(required)
}

func (tm *TrackerManager) fetchTrackersForHash(ctx context.Context, hash string) ([]TorrentTracker, error) {
	if tm == nil || tm.api == nil {
		return nil, fmt.Errorf("tracker manager not initialized")
	}

	return tm.api.GetTorrentTrackersCtx(ctx, hash)
}
