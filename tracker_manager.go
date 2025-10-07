package qbittorrent

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/Masterminds/semver"
	"github.com/autobrr/autobrr/pkg/ttlcache"
)

const (
	trackerCacheTTL = 30 * time.Minute
	// trackerIncludeChunkSize bounds how many hashes we ask qBittorrent to enrich in a single
	// includeTrackers request. Keeping the chunk small prevents oversized URLs/payloads that
	// can time out or blow past server limits.
	trackerIncludeChunkSize = 50
	// trackerIncludeFetchAllThreshold controls when we skip hash-specific batching and request
	// tracker data for every torrent in a single call. This avoids issuing hundreds of requests
	// (each scanning the full torrent list anyway) when the missing set is large.
	trackerIncludeFetchAllThreshold = 200
)

// trackerAPI describes the subset of Client functionality required by TrackerManager.
type trackerAPI interface {
	GetTorrentsCtx(ctx context.Context, o TorrentFilterOptions) ([]Torrent, error)
	getApiVersion() (*semver.Version, error)
}

// TrackerManager coordinates tracker metadata hydration with caching.
type TrackerManager struct {
	api             trackerAPI
	cache           *ttlcache.Cache[string, []TorrentTracker]
	includeTrackers bool
}

// TrackerHydrateOptions configure how torrents are hydrated with tracker metadata.
type TrackerHydrateOptions struct {
	AllowFetch bool
}

// TrackerHydrateOption applies functional options to TrackerHydrateOptions.
type TrackerHydrateOption func(*TrackerHydrateOptions)

// WithTrackerAllowFetch toggles whether remote fetches are allowed when data is missing.
func WithTrackerAllowFetch(allow bool) TrackerHydrateOption {
	return func(opts *TrackerHydrateOptions) {
		opts.AllowFetch = allow
	}
}

func defaultTrackerHydrateOptions() TrackerHydrateOptions {
	return TrackerHydrateOptions{
		AllowFetch: true,
	}
}

// NewTrackerManager constructs a manager backed by the provided API client.
func NewTrackerManager(api trackerAPI) *TrackerManager {
	if api == nil {
		return nil
	}

	manager := &TrackerManager{
		api:   api,
		cache: ttlcache.New(ttlcache.Options[string, []TorrentTracker]{}.SetDefaultTTL(trackerCacheTTL)),
	}

	manager.includeTrackers = manager.detectIncludeTrackers()

	return manager
}

// SupportsIncludeTrackers reports whether the server can embed tracker data in torrent payloads.
func (tm *TrackerManager) SupportsIncludeTrackers() bool {
	if tm == nil {
		return false
	}
	return tm.includeTrackers
}

// HydrateTorrents enriches the provided torrents with tracker metadata.
// It returns the enriched slice, a cache of tracker lists keyed by hash, remaining hashes,
// and the first error observed while fetching trackers.
func (tm *TrackerManager) HydrateTorrents(ctx context.Context, torrents []Torrent, opts ...TrackerHydrateOption) ([]Torrent, map[string][]TorrentTracker, []string, error) {
	if tm == nil || tm.api == nil || len(torrents) == 0 {
		return torrents, nil, nil, nil
	}

	options := defaultTrackerHydrateOptions()
	for _, opt := range opts {
		opt(&options)
	}

	trackerMap := make(map[string][]TorrentTracker, len(torrents))
	needed := make([]string, 0, len(torrents))

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

		needed = append(needed, hash)
	}

	if len(needed) == 0 {
		return torrents, trackerMap, nil, nil
	}

	trackerData, remaining, err := tm.getTrackersForHashes(ctx, needed, options.AllowFetch)
	if len(trackerData) > 0 {
		maps.Copy(trackerMap, trackerData)
		for i := range torrents {
			if trackers, ok := trackerMap[torrents[i].Hash]; ok {
				torrents[i].Trackers = trackers
			}
		}
	}

	return torrents, trackerMap, remaining, err
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

func (tm *TrackerManager) getTrackersForHashes(ctx context.Context, hashes []string, allowFetch bool) (map[string][]TorrentTracker, []string, error) {
	result := make(map[string][]TorrentTracker, len(hashes))
	if tm == nil || tm.api == nil {
		return result, deduplicateHashes(hashes), fmt.Errorf("tracker manager is not initialized")
	}

	deduped := deduplicateHashes(hashes)
	missing := make([]string, 0, len(deduped))

	for _, hash := range deduped {
		if trackers, ok := tm.cache.Get(hash); ok {
			result[hash] = trackers
			continue
		}
		missing = append(missing, hash)
	}

	if len(missing) == 0 || !allowFetch {
		return result, missing, nil
	}

	fetchAll := tm.includeTrackers && len(missing) >= trackerIncludeFetchAllThreshold
	var toFetch []string
	if !fetchAll {
		toFetch = missing
	}

	var remaining []string

	fetched, err := tm.fetchAndCacheTrackers(ctx, toFetch)
	if len(fetched) > 0 {
		maps.Copy(result, fetched)
	}

	if fetchAll {
		for _, hash := range missing {
			if _, ok := fetched[hash]; !ok {
				remaining = append(remaining, hash)
			}
		}
	} else {
		for _, hash := range toFetch {
			if _, ok := fetched[hash]; !ok {
				remaining = append(remaining, hash)
			}
		}
	}

	return result, remaining, err
}

func (tm *TrackerManager) fetchAndCacheTrackers(ctx context.Context, hashes []string) (map[string][]TorrentTracker, error) {
	if !tm.includeTrackers {
		return map[string][]TorrentTracker{}, fmt.Errorf("includeTrackers support required")
	}

	var deduped []string
	if len(hashes) > 0 {
		deduped = deduplicateHashes(hashes)
		if len(deduped) == 0 {
			return map[string][]TorrentTracker{}, nil
		}
	}

	fetched, err := tm.fetchTrackersViaInclude(ctx, deduped)

	if len(fetched) > 0 {
		for hash, trackers := range fetched {
			tm.cache.Set(hash, trackers, ttlcache.DefaultTTL)
		}
	}

	return fetched, err
}

func (tm *TrackerManager) fetchTrackersViaInclude(ctx context.Context, hashes []string) (map[string][]TorrentTracker, error) {
	result := make(map[string][]TorrentTracker, len(hashes))
	if len(hashes) == 0 {
		torrents, err := tm.api.GetTorrentsCtx(ctx, TorrentFilterOptions{IncludeTrackers: true})
		if err != nil {
			return result, err
		}

		result = make(map[string][]TorrentTracker, len(torrents))
		for _, torrent := range torrents {
			trackers := torrent.Trackers
			if trackers == nil {
				trackers = []TorrentTracker{}
			}
			result[torrent.Hash] = trackers
		}

		return result, nil
	}

	var firstErr error

	for start := 0; start < len(hashes); start += trackerIncludeChunkSize {
		end := start + trackerIncludeChunkSize
		if end > len(hashes) {
			end = len(hashes)
		}

		opts := TorrentFilterOptions{Hashes: hashes[start:end], IncludeTrackers: true}
		torrents, err := tm.api.GetTorrentsCtx(ctx, opts)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		for _, torrent := range torrents {
			trackers := torrent.Trackers
			if trackers == nil {
				trackers = []TorrentTracker{}
			}
			result[torrent.Hash] = trackers
		}

		for _, hash := range opts.Hashes {
			if _, ok := result[hash]; !ok {
				result[hash] = []TorrentTracker{}
			}
		}
	}

	return result, firstErr
}

func deduplicateHashes(hashes []string) []string {
	if len(hashes) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(hashes))
	unique := make([]string, 0, len(hashes))
	for _, hash := range hashes {
		hash = strings.TrimSpace(hash)
		if hash == "" {
			continue
		}
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		unique = append(unique, hash)
	}

	return unique
}
