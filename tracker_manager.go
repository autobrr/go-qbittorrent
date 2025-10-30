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
	trackerCacheTTL         = 30 * time.Minute
	trackerIncludeChunkSize = 100
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

	if len(hashesToFetch) == 0 {
		return torrents, trackerMap
	}

	// Fast path: fetch trackers with includeTrackers support when available
	if tm.SupportsIncludeTrackers() {
		tm.hydrateWithIncludeTrackers(ctx, torrents, trackerMap, hashesToFetch, hashToTorrentIndex)
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

func (tm *TrackerManager) hydrateWithIncludeTrackers(ctx context.Context, torrents []Torrent, trackerMap map[string][]TorrentTracker, hashes []string, hashToTorrentIndex map[string]int) {
	pending := make(map[string]struct{}, len(hashes))
	ordered := make([]string, 0, len(hashes))
	for _, hash := range hashes {
		hash = strings.TrimSpace(hash)
		if hash == "" {
			continue
		}
		if _, exists := pending[hash]; exists {
			continue
		}
		pending[hash] = struct{}{}
		ordered = append(ordered, hash)
	}

	if len(pending) == 0 {
		return
	}

	// applyFetched records tracker responses on the relevant torrent entries and returns
	// how many of the outstanding hashes were satisfied by this batch.
	applyFetched := func(fetched []Torrent) int {
		progress := 0
		for _, fetched := range fetched {
			hash := strings.TrimSpace(fetched.Hash)
			if hash == "" {
				continue
			}

			if idx, ok := hashToTorrentIndex[hash]; ok {
				torrents[idx].Trackers = fetched.Trackers
				trackerMap[hash] = fetched.Trackers
				tm.cache.Set(hash, fetched.Trackers, calculateTrackerTTL(fetched.Reannounce))
			}

			if _, ok := pending[hash]; ok {
				delete(pending, hash)
				progress++
			}
		}
		return progress
	}

	// Try fetching all hashes in one request first for environments without proxy limits. If that
	// works we skip the chunking overhead entirely.
	if fetchedTorrents, err := tm.api.GetTorrentsCtx(ctx, TorrentFilterOptions{
		Hashes:          ordered,
		IncludeTrackers: true,
	}); err == nil {
		applyFetched(fetchedTorrents)
		if len(pending) == 0 {
			return
		}
	}

	// Fallback that relies on qBittorrent returning all torrents when no hash filter is provided.
	fetchAll := func() {
		fetchedTorrents, err := tm.api.GetTorrentsCtx(ctx, TorrentFilterOptions{IncludeTrackers: true})
		if err != nil {
			return
		}
		applyFetched(fetchedTorrents)
	}

	for len(pending) > 0 {
		chunk := make([]string, 0, minInt(len(pending), trackerIncludeChunkSize))
		for hash := range pending {
			chunk = append(chunk, hash)
			if len(chunk) >= trackerIncludeChunkSize {
				break
			}
		}

		fetchedTorrents, err := tm.api.GetTorrentsCtx(ctx, TorrentFilterOptions{
			Hashes:          chunk,
			IncludeTrackers: true,
		})
		if err != nil {
			fetchAll()
			return
		}

		if progress := applyFetched(fetchedTorrents); progress == 0 {
			fetchAll()
			return
		}
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
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
