package qbittorrent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeTrackerClient struct {
	mu    sync.Mutex
	calls map[string]int
	data  map[string][]TorrentTracker
	err   map[string]error
}

func newFakeTrackerClient(data map[string][]TorrentTracker, err map[string]error) *fakeTrackerClient {
	return &fakeTrackerClient{
		calls: make(map[string]int),
		data:  data,
		err:   err,
	}
}

func (f *fakeTrackerClient) GetTorrentTrackersCtx(ctx context.Context, hash string) ([]TorrentTracker, error) {
	f.mu.Lock()
	f.calls[hash]++
	f.mu.Unlock()

	if err, ok := f.err[hash]; ok {
		return nil, err
	}

	if trackers, ok := f.data[hash]; ok {
		return trackers, nil
	}

	return nil, nil
}

func TestTrackerFetcherFetch(t *testing.T) {
	trackers := map[string][]TorrentTracker{
		"hashA": {{Url: "udp://tracker.one", Status: TrackerStatusOK}},
		"hashB": {{Url: "udp://tracker.two", Status: TrackerStatusNotWorking}},
		"hashC": {},
	}

	client := newFakeTrackerClient(trackers, nil)
	fetcher := NewTrackerFetcher(client, WithTrackerFetcherConcurrency(2))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	result, err := fetcher.Fetch(ctx, []string{"hashA", "hashB", "hashA", "hashC", "hashC"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}

	for hash, expected := range trackers {
		got, ok := result[hash]
		if !ok {
			t.Fatalf("missing trackers for %s", hash)
		}
		if len(got) != len(expected) {
			t.Fatalf("expected %d trackers for %s, got %d", len(expected), hash, len(got))
		}
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if client.calls["hashA"] != 1 {
		t.Fatalf("expected hashA to be fetched once, got %d", client.calls["hashA"])
	}
	if client.calls["hashC"] != 1 {
		t.Fatalf("expected hashC to be fetched once, got %d", client.calls["hashC"])
	}
}

func TestTrackerFetcherFetchWithErrors(t *testing.T) {
	errSentinel := errors.New("boom")
	data := map[string][]TorrentTracker{
		"good": {{Url: "udp://ok", Status: TrackerStatusOK}},
	}
	client := newFakeTrackerClient(data, map[string]error{
		"bad": errSentinel,
	})

	fetcher := NewTrackerFetcher(client)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	result, err := fetcher.Fetch(ctx, []string{"good", "bad"})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	if !errors.Is(err, errSentinel) {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := result["good"]; !ok {
		t.Fatalf("expected successful result for good hash")
	}
}

func TestTrackerFetcherContextCancel(t *testing.T) {
	client := newFakeTrackerClient(nil, nil)
	fetcher := NewTrackerFetcher(client, WithTrackerFetcherConcurrency(1))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := fetcher.Fetch(ctx, []string{"hash"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
