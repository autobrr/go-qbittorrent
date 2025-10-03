package qbittorrent

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver"
	"github.com/autobrr/autobrr/pkg/ttlcache"
)

const (
	// TrackerFetchUnlimited disables fetch limiting when hydrating tracker metadata.
	TrackerFetchUnlimited     = -1
	trackerCacheTTL           = 30 * time.Minute
	trackerFetchChunkDefault  = 300
	trackerIncludeChunkSize   = 50
	trackerWarmupBatchSize    = 1000
	trackerWarmupDelay        = 2 * time.Second
	trackerWarmupTimeout      = 45 * time.Second
	trackerFetcherConcurrency = 4
)

// trackerAPI describes the subset of Client functionality required by TrackerManager.
type trackerAPI interface {
	GetTorrentTrackersCtx(ctx context.Context, hash string) ([]TorrentTracker, error)
	GetTorrentsCtx(ctx context.Context, o TorrentFilterOptions) ([]Torrent, error)
	getApiVersion() (*semver.Version, error)
}

// TrackerManager coordinates tracker metadata hydration with caching and warmup.
type TrackerManager struct {
	api             trackerAPI
	cache           *ttlcache.Cache[string, []TorrentTracker]
	fetcher         *trackerFetcher
	includeTrackers bool

	fetcherMu     sync.Mutex
	warmupMu      sync.Mutex
	warmupPending map[string]struct{}
}

// TrackerHydrateOptions configure how torrents are hydrated with tracker metadata.
type TrackerHydrateOptions struct {
	FetchLimit int
	AllowFetch bool
	Warmup     bool
}

// TrackerHydrateOption applies functional options to TrackerHydrateOptions.
type TrackerHydrateOption func(*TrackerHydrateOptions)

// WithTrackerFetchLimit sets the maximum number of hashes to fetch in one pass.
func WithTrackerFetchLimit(limit int) TrackerHydrateOption {
	return func(opts *TrackerHydrateOptions) {
		opts.FetchLimit = limit
	}
}

// WithTrackerAllowFetch toggles whether remote fetches are allowed when data is missing.
func WithTrackerAllowFetch(allow bool) TrackerHydrateOption {
	return func(opts *TrackerHydrateOptions) {
		opts.AllowFetch = allow
	}
}

// WithTrackerWarmup toggles whether remaining hashes should be processed asynchronously.
func WithTrackerWarmup(enabled bool) TrackerHydrateOption {
	return func(opts *TrackerHydrateOptions) {
		opts.Warmup = enabled
	}
}

func defaultTrackerHydrateOptions() TrackerHydrateOptions {
	return TrackerHydrateOptions{
		FetchLimit: 0,
		AllowFetch: true,
		Warmup:     true,
	}
}

// NewTrackerManager constructs a manager backed by the provided API client.
func NewTrackerManager(api trackerAPI) *TrackerManager {
	if api == nil {
		return nil
	}

	manager := &TrackerManager{
		api:           api,
		cache:         ttlcache.New(ttlcache.Options[string, []TorrentTracker]{}.SetDefaultTTL(trackerCacheTTL)),
		warmupPending: make(map[string]struct{}),
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

	trackerData, remaining, err := tm.getTrackersForHashes(ctx, needed, options.AllowFetch, options.FetchLimit)
	if len(trackerData) > 0 {
		maps.Copy(trackerMap, trackerData)
		for i := range torrents {
			if trackers, ok := trackerMap[torrents[i].Hash]; ok {
				torrents[i].Trackers = trackers
			}
		}
	}

	// Warmup path only mattered for legacy tracker fetches; disable for now while we rely on IncludeTrackers.
	// if options.Warmup && options.AllowFetch && len(remaining) > 0 {
	// 	tm.scheduleWarmup(remaining, trackerWarmupBatchSize)
	// }

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

func (tm *TrackerManager) getTrackersForHashes(ctx context.Context, hashes []string, allowFetch bool, fetchLimit int) (map[string][]TorrentTracker, []string, error) {
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

	if fetchLimit == TrackerFetchUnlimited {
		fetchLimit = 0
	}

	// Legacy fallback (chunked fetch via tracker API) disabled for now – we only support servers with IncludeTrackers.
	toFetch := missing
	var remaining []string

	fetched, err := tm.fetchAndCacheTrackers(ctx, toFetch)
	if len(fetched) > 0 {
		maps.Copy(result, fetched)
	}

	for _, hash := range toFetch {
		if _, ok := fetched[hash]; !ok {
			remaining = append(remaining, hash)
		}
	}

	return result, remaining, err
}

func (tm *TrackerManager) fetchAndCacheTrackers(ctx context.Context, hashes []string) (map[string][]TorrentTracker, error) {
	hashes = deduplicateHashes(hashes)
	if len(hashes) == 0 {
		return map[string][]TorrentTracker{}, nil
	}

	var (
		fetched map[string][]TorrentTracker
		err     error
	)

	if tm.includeTrackers {
		fetched, err = tm.fetchTrackersViaInclude(ctx, hashes)
	} else {
		// Legacy tracker fetch via individual GetTorrentTrackers calls is disabled for now.
		return map[string][]TorrentTracker{}, fmt.Errorf("includeTrackers support required")
	}

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

func (tm *TrackerManager) ensureFetcher() *trackerFetcher {
	tm.fetcherMu.Lock()
	defer tm.fetcherMu.Unlock()

	if tm.fetcher == nil {
		tm.fetcher = newTrackerFetcher(tm.api, trackerFetcherConcurrency)
	}

	return tm.fetcher
}

func (tm *TrackerManager) scheduleWarmup(hashes []string, batchSize int) {
	hashes = deduplicateHashes(hashes)
	if len(hashes) == 0 {
		return
	}

	pending := make([]string, 0, len(hashes))
	for _, hash := range hashes {
		if _, ok := tm.cache.Get(hash); ok {
			continue
		}
		pending = append(pending, hash)
	}

	if len(pending) == 0 {
		return
	}

	tm.warmupMu.Lock()
	filtered := make([]string, 0, len(pending))
	for _, hash := range pending {
		if _, exists := tm.warmupPending[hash]; exists {
			continue
		}
		tm.warmupPending[hash] = struct{}{}
		filtered = append(filtered, hash)
	}
	tm.warmupMu.Unlock()

	if len(filtered) == 0 {
		return
	}

	if batchSize <= 0 {
		batchSize = trackerWarmupBatchSize
	}

	go tm.runWarmup(filtered, batchSize)
}

func (tm *TrackerManager) runWarmup(hashes []string, batchSize int) {
	defer func() {
		tm.warmupMu.Lock()
		for _, hash := range hashes {
			delete(tm.warmupPending, hash)
		}
		tm.warmupMu.Unlock()
	}()

	for start := 0; start < len(hashes); start += batchSize {
		end := start + batchSize
		if end > len(hashes) {
			end = len(hashes)
		}

		ctx, cancel := context.WithTimeout(context.Background(), trackerWarmupTimeout)
		_, _ = tm.fetchAndCacheTrackers(ctx, hashes[start:end])
		cancel()

		if end < len(hashes) {
			time.Sleep(trackerWarmupDelay)
		}
	}
}

// trackerFetcher performs bounded-concurrency tracker lookups when includeTrackers support is unavailable.
type trackerFetcher struct {
	client        trackerFetcherClient
	maxConcurrent int
}

type trackerFetcherClient interface {
	GetTorrentTrackersCtx(ctx context.Context, hash string) ([]TorrentTracker, error)
}

func newTrackerFetcher(client trackerFetcherClient, concurrency int) *trackerFetcher {
	if concurrency <= 0 {
		concurrency = 1
	}
	return &trackerFetcher{
		client:        client,
		maxConcurrent: concurrency,
	}
}

func (tf *trackerFetcher) Fetch(ctx context.Context, hashes []string) (map[string][]TorrentTracker, error) {
	if tf == nil || tf.client == nil {
		return nil, fmt.Errorf("tracker fetcher is not initialized")
	}

	unique := deduplicateHashes(hashes)
	if len(unique) == 0 {
		return map[string][]TorrentTracker{}, nil
	}

	results := make(map[string][]TorrentTracker, len(unique))
	var resultsMu sync.Mutex

	throttle := make(chan struct{}, tf.maxConcurrent)
	var wg sync.WaitGroup

	var firstErr error
	var errOnce sync.Once

Loop:
	for _, hash := range unique {
		select {
		case <-ctx.Done():
			errOnce.Do(func() {
				firstErr = ctx.Err()
			})
			break Loop
		default:
		}

		wg.Add(1)
		go func(hash string) {
			defer wg.Done()

			select {
			case throttle <- struct{}{}:
			case <-ctx.Done():
				errOnce.Do(func() {
					firstErr = ctx.Err()
				})
				return
			}
			defer func() { <-throttle }()

			trackers, err := tf.client.GetTorrentTrackersCtx(ctx, hash)
			if err != nil {
				if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
					errOnce.Do(func() {
						firstErr = err
					})
				}
				return
			}

			if trackers == nil {
				trackers = []TorrentTracker{}
			}

			resultsMu.Lock()
			results[hash] = trackers
			resultsMu.Unlock()
		}(hash)
	}

	wg.Wait()

	return results, firstErr
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
