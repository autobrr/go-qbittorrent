package qbittorrent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// trackerClient abstracts the subset of *Client used by the tracker fetcher.
type trackerClient interface {
	GetTorrentTrackersCtx(ctx context.Context, hash string) ([]TorrentTracker, error)
}

// TrackerFetcherOption configures the tracker fetcher.
type TrackerFetcherOption func(*TrackerFetcher)

const defaultTrackerFetcherConcurrency = 4

// TrackerFetcher performs bounded-concurrency tracker lookups for a batch of hashes.
type TrackerFetcher struct {
	client        trackerClient
	maxConcurrent int
}

// NewTrackerFetcher creates a tracker fetcher for the provided client.
func NewTrackerFetcher(client trackerClient, opts ...TrackerFetcherOption) *TrackerFetcher {
	tf := &TrackerFetcher{
		client:        client,
		maxConcurrent: defaultTrackerFetcherConcurrency,
	}

	for _, opt := range opts {
		opt(tf)
	}

	if tf.maxConcurrent <= 0 {
		tf.maxConcurrent = 1
	}

	return tf
}

// WithTrackerFetcherConcurrency overrides the maximum number of in-flight tracker requests.
func WithTrackerFetcherConcurrency(n int) TrackerFetcherOption {
	return func(tf *TrackerFetcher) {
		tf.maxConcurrent = n
	}
}

// Fetch returns tracker metadata for the supplied torrent hashes.
// It deduplicates hashes, enforces a concurrency limit, and continues work even if
// individual hashes fail (the first error encountered is returned alongside any
// successful results).
func (tf *TrackerFetcher) Fetch(ctx context.Context, hashes []string) (map[string][]TorrentTracker, error) {
	if tf == nil || tf.client == nil {
		return nil, fmt.Errorf("tracker fetcher is not initialized")
	}

	unique := tf.deduplicate(hashes)
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

	if firstErr != nil {
		return results, firstErr
	}

	return results, nil
}

func (tf *TrackerFetcher) deduplicate(hashes []string) []string {
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
