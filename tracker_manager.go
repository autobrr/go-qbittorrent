package qbittorrent

import (
	"context"
	"fmt"
	"strings"
	"sync"
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
		cache: ttlcache.New(ttlcache.Options[string, []TorrentTracker]{}.SetDefaultTTL(trackerCacheTTL).DisableUpdateTime(true)),
	}

	return manager
}

// HydrateTorrents enriches the provided torrents with tracker metadata from cache.
// For versions that support IncludeTrackers, fetches all trackers at once.
// Otherwise fetches individually if not cached.
// It returns the enriched slice and a cache of tracker lists keyed by hash.
func (tm *TrackerManager) HydrateTorrents(ctx context.Context, torrents []Torrent) ([]Torrent, map[string][]TorrentTracker) {
	if tm == nil || len(torrents) == 0 {
		return torrents, nil
	}

	trackerMap := make(map[string][]TorrentTracker, len(torrents))
	hashesToFetch := []string{}
	hashToTorrentIndex := make(map[string]int)

	// First pass: collect hashes that need fetching
	for i := range torrents {
		hash := strings.TrimSpace(torrents[i].Hash)
		if hash == "" {
			continue
		}

		hashToTorrentIndex[hash] = i

		if len(torrents[i].Trackers) > 0 {
			trackerMap[hash] = torrents[i].Trackers
			continue
		}

		if trackers, ok := tm.cache.Get(hash); ok {
			torrents[i].Trackers = trackers
			trackerMap[hash] = trackers
			continue
		}

		// Need to fetch this hash
		hashesToFetch = append(hashesToFetch, hash)
	}

	// Fast path: fetch all trackers at once if supported
	if tm.SupportsIncludeTrackers() {
		hashList := hashesToFetch

		if fetchedTorrents, err := tm.api.GetTorrentsCtx(ctx, TorrentFilterOptions{
			Hashes:          hashList,
			IncludeTrackers: true,
		}); err == nil {
			// Update torrents and cache
			for _, fetched := range fetchedTorrents {
				hash := strings.TrimSpace(fetched.Hash)
				if len(hash) == 0 {
					continue
				}

				i, ok := hashToTorrentIndex[hash]
				if !ok {
					continue
				}

				torrents[i].Trackers = fetched.Trackers
				trackerMap[hash] = fetched.Trackers
				tm.cache.Set(hash, fetched.Trackers, calculateTrackerTTL(fetched.Reannounce))
			}
		}
	} else {
		// Fetch hashes individually (fallback when fast path not supported)
		// Use pipelining to fetch in parallel for better performance
		type fetchResult struct {
			hash     string
			trackers []TorrentTracker
			err      error
		}

		results := make(chan fetchResult, len(hashesToFetch))
		sem := make(chan struct{}, 50) // Limit concurrency to 50
		var wg sync.WaitGroup

		wg.Add(len(hashesToFetch))
		for _, hash := range hashesToFetch {
			sem <- struct{}{} // Acquire semaphore before starting goroutine
			go func(h string) {
				defer wg.Done()
				defer func() { <-sem }() // Release semaphore

				select {
				case <-ctx.Done():
					results <- fetchResult{hash: h, err: ctx.Err()}
					return
				default:
				}

				trackers, err := tm.fetchTrackersForHash(ctx, h)
				results <- fetchResult{hash: h, trackers: trackers, err: err}
			}(hash)
		}

		// Close results channel after all goroutines finish
		go func() {
			wg.Wait()
			close(results)
		}()

		// Collect results
		for res := range results {
			if res.err == nil && len(res.trackers) > 0 {
				i := hashToTorrentIndex[res.hash]
				torrents[i].Trackers = res.trackers
				trackerMap[res.hash] = res.trackers
				tm.cache.Set(res.hash, res.trackers, calculateTrackerTTL(torrents[i].Reannounce))
			}
		}
	}

	return torrents, trackerMap
}

// calculateTrackerTTL calculates the appropriate TTL for tracker cache based on reannounce time
func calculateTrackerTTL(reannounce int64) time.Duration {
	ttl := trackerCacheTTL

	if reannounce > 0 {
		// Use the reannounce time, but cap at maximum TTL
		if reannounceDuration := time.Duration(reannounce) * time.Second; reannounceDuration < ttl {
			ttl = reannounceDuration
		}
	}

	return ttl
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

func (tm *TrackerManager) SupportsIncludeTrackers() bool {
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
