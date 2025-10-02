package qbittorrent

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/Masterminds/semver"
)

type fakeTrackerAPI struct {
	mu         sync.Mutex
	include    bool
	trackers   map[string][]TorrentTracker
	trackerErr map[string]error
	callCount  map[string]int
}

func newFakeTrackerAPI(include bool, trackers map[string][]TorrentTracker, errs map[string]error) *fakeTrackerAPI {
	return &fakeTrackerAPI{
		include:    include,
		trackers:   trackers,
		trackerErr: errs,
		callCount:  make(map[string]int),
	}
}

func (f *fakeTrackerAPI) getApiVersion() (*semver.Version, error) {
	if f.include {
		return semver.MustParse("2.11.4"), nil
	}
	return semver.MustParse("2.11.0"), nil
}

func (f *fakeTrackerAPI) GetTorrentTrackersCtx(ctx context.Context, hash string) ([]TorrentTracker, error) {
	f.mu.Lock()
	f.callCount[hash]++
	f.mu.Unlock()

	if err, ok := f.trackerErr[hash]; ok {
		return nil, err
	}

	if trackers, ok := f.trackers[hash]; ok {
		return trackers, nil
	}

	return nil, nil
}

func (f *fakeTrackerAPI) GetTorrentsCtx(ctx context.Context, opts TorrentFilterOptions) ([]Torrent, error) {
	torrents := make([]Torrent, 0, len(opts.Hashes))
	for _, hash := range opts.Hashes {
		trackers := f.trackers[hash]
		if trackers == nil {
			trackers = []TorrentTracker{}
		}
		torrents = append(torrents, Torrent{Hash: hash, Trackers: trackers})
	}
	return torrents, nil
}

func TestTrackerManagerHydrateFallback(t *testing.T) {
	data := map[string][]TorrentTracker{
		"hashA": {
			{Url: "udp://one", Status: TrackerStatusOK},
		},
		"hashB": {
			{Url: "udp://two", Status: TrackerStatusNotWorking},
		},
	}
	api := newFakeTrackerAPI(false, data, nil)
	manager := NewTrackerManager(api)

	torrents := []Torrent{{Hash: "hashA"}, {Hash: "hashB"}, {Hash: "hashA"}}
	ctx := context.Background()

	enriched, trackerMap, remaining, err := manager.HydrateTorrents(ctx, torrents)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(remaining) != 0 {
		t.Fatalf("expected no remaining hashes, got %v", remaining)
	}

	if len(trackerMap) != 2 {
		t.Fatalf("expected 2 tracker entries, got %d", len(trackerMap))
	}

	if manager.fetcher == nil {
		t.Fatalf("expected fetcher to be initialized for fallback path")
	}

	api.mu.Lock()
	callsA := api.callCount["hashA"]
	callsB := api.callCount["hashB"]
	api.mu.Unlock()

	if callsA != 1 {
		t.Fatalf("expected hashA fetched once, got %d", callsA)
	}
	if callsB != 1 {
		t.Fatalf("expected hashB fetched once, got %d", callsB)
	}

	if len(enriched) != 3 {
		t.Fatalf("expected 3 torrents, got %d", len(enriched))
	}
	if len(enriched[0].Trackers) == 0 {
		t.Fatalf("expected tracker data on first torrent")
	}
}

func TestTrackerManagerHydrateInclude(t *testing.T) {
	data := map[string][]TorrentTracker{
		"good": {
			{Url: "udp://ok", Status: TrackerStatusOK},
		},
	}
	api := newFakeTrackerAPI(true, data, nil)
	manager := NewTrackerManager(api)

	if !manager.SupportsIncludeTrackers() {
		t.Fatalf("expected includeTrackers support to be detected")
	}

	torrents := []Torrent{{Hash: "good"}}
	ctx := context.Background()

	enriched, trackerMap, remaining, err := manager.HydrateTorrents(ctx, torrents)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(remaining) != 0 {
		t.Fatalf("expected no remaining hashes, got %v", remaining)
	}

	if manager.fetcher != nil {
		t.Fatalf("did not expect fetcher initialization when includeTrackers supported")
	}

	if len(trackerMap["good"]) != 1 {
		t.Fatalf("expected trackers for good hash")
	}

	if len(enriched[0].Trackers) != 1 {
		t.Fatalf("expected enriched torrent to include trackers")
	}
}

func TestTrackerManagerHydrateError(t *testing.T) {
	sentinel := errors.New("boom")
	data := map[string][]TorrentTracker{
		"good": {
			{Url: "udp://ok", Status: TrackerStatusOK},
		},
	}
	errs := map[string]error{"bad": sentinel}
	api := newFakeTrackerAPI(false, data, errs)
	manager := NewTrackerManager(api)

	torrents := []Torrent{{Hash: "good"}, {Hash: "bad"}}
	ctx := context.Background()

	_, trackerMap, remaining, err := manager.HydrateTorrents(ctx, torrents)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}

	if len(trackerMap["good"]) != 1 {
		t.Fatalf("expected good hash to hydrate tracker data")
	}

	found := false
	for _, hash := range remaining {
		if hash == "bad" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected bad hash in remaining list, got %v", remaining)
	}
}
