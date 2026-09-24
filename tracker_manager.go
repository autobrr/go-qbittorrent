package qbittorrent

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"github.com/autobrr/go-qbittorrent/pkg/ttlcache"
)

const (
	trackerCacheTTL         = 5 * time.Minute
	trackerIncludeChunkSize = 100
)

// trackerAPI describes the subset of Client functionality required by TrackerManager.
type trackerAPI interface {
	GetTorrentsCtx(ctx context.Context, o TorrentFilterOptions) ([]Torrent, error)
}

// TrackerManager coordinates tracker metadata hydration with caching.
type TrackerManager struct {
	api                trackerAPI
	cache              *ttlcache.Cache[string, []TorrentTracker]
	useIncludeTrackers atomic.Bool
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
// When IncludeTrackers is supported, it fetches the hashes that are not cached.
// Otherwise it applies only the cached entries.
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

	if tm.SupportsIncludeTrackers() {
		tm.hydrateWithIncludeTrackers(ctx, torrents, trackerMap, hashesToFetch, hashToTorrentIndex)
	}

	return torrents, trackerMap
}

// Refresh fetches tracker metadata for the whole library in one request and writes it
// to the cache. Use it for a periodic pass over the whole library.
// The request has no hash filter, so the URL stays short for any library size.
// A failed fetch does not empty the cache, so other readers still get data.
// Refresh ignores the cache and the trackers already on the torrents.
// A torrent that the fetch does not return keeps its current Trackers and is absent
// from the returned map. When the fetch fails, Refresh returns the torrents unchanged
// and the error.
// When IncludeTrackers is not supported, it returns the torrents unchanged and no error.
func (tm *TrackerManager) Refresh(ctx context.Context, torrents []Torrent) ([]Torrent, map[string][]TorrentTracker, error) {
	if tm == nil || len(torrents) == 0 || !tm.SupportsIncludeTrackers() {
		return torrents, nil, nil
	}

	fetched, err := tm.api.GetTorrentsCtx(ctx, TorrentFilterOptions{IncludeTrackers: true})
	if err != nil {
		return torrents, nil, err
	}

	byHash := make(map[string][]TorrentTracker, len(fetched))
	for _, torrent := range fetched {
		hash := strings.TrimSpace(torrent.Hash)
		if hash == "" {
			continue
		}
		byHash[hash] = torrent.Trackers
		tm.cache.Set(hash, torrent.Trackers, trackerCacheTTL)
	}

	trackerMap := make(map[string][]TorrentTracker, len(torrents))
	for i := range torrents {
		hash := strings.TrimSpace(torrents[i].Hash)
		if trackers, ok := byHash[hash]; ok {
			torrents[i].Trackers = trackers
			trackerMap[hash] = trackers
		}
	}
	return torrents, trackerMap, nil
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
				tm.cache.Set(hash, fetched.Trackers, trackerCacheTTL)
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
		chunk := make([]string, 0, min(len(pending), trackerIncludeChunkSize))
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

// SetUseIncludeTrackers configures whether the manager should use the bulk
// IncludeTrackers API (available in qBittorrent 5.1+/WebAPI 2.11.4+).
func (tm *TrackerManager) SetUseIncludeTrackers(use bool) {
	if tm == nil {
		return
	}
	tm.useIncludeTrackers.Store(use)
}

// SupportsIncludeTrackers reports whether bulk tracker fetching is enabled.
func (tm *TrackerManager) SupportsIncludeTrackers() bool {
	if tm == nil {
		return false
	}
	return tm.useIncludeTrackers.Load()
}
