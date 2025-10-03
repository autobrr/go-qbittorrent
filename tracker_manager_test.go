package qbittorrent

import (
	"context"
	"errors"
	"testing"

	"github.com/Masterminds/semver"
)

type fakeTrackerAPI struct {
	include    bool
	trackers   map[string][]TorrentTracker
	trackerErr map[string]error
}

func newFakeTrackerAPI(include bool, trackers map[string][]TorrentTracker, errs map[string]error) *fakeTrackerAPI {
	return &fakeTrackerAPI{
		include:    include,
		trackers:   trackers,
		trackerErr: errs,
	}
}

func (f *fakeTrackerAPI) getApiVersion() (*semver.Version, error) {
	if f.include {
		return semver.MustParse("2.11.4"), nil
	}
	return semver.MustParse("2.11.0"), nil
}

func (f *fakeTrackerAPI) GetTorrentTrackersCtx(ctx context.Context, hash string) ([]TorrentTracker, error) {
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
	var firstErr error
	for _, hash := range opts.Hashes {
		if err, ok := f.trackerErr[hash]; ok {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		trackers := f.trackers[hash]
		if trackers == nil {
			trackers = []TorrentTracker{}
		}
		torrents = append(torrents, Torrent{Hash: hash, Trackers: trackers})
	}
	return torrents, firstErr
}

func TestTrackerManagerHydrateRequiresIncludeSupport(t *testing.T) {
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
	if err == nil {
		t.Fatalf("expected error when includeTrackers unsupported")
	}

	if err.Error() != "includeTrackers support required" {
		t.Fatalf("expected includeTrackers support error, got %v", err)
	}

	if len(remaining) != 2 {
		t.Fatalf("expected remaining hashes, got %v", remaining)
	}

	if len(trackerMap) != 0 {
		t.Fatalf("expected no tracker data, got %v", trackerMap)
	}

	if len(enriched) != 3 {
		t.Fatalf("expected original torrent slice to be returned")
	}
	for _, torrent := range enriched {
		if len(torrent.Trackers) != 0 {
			t.Fatalf("expected torrent %s to remain without trackers", torrent.Hash)
		}
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
	api := newFakeTrackerAPI(true, data, errs)
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

	if len(remaining) != 0 {
		t.Fatalf("expected no remaining hashes, got %v", remaining)
	}

	if trackers := trackerMap["good"]; len(trackers) != 1 {
		t.Fatalf("expected good hash to hydrate tracker data")
	}

	if trackers := trackerMap["bad"]; len(trackers) != 0 {
		t.Fatalf("expected bad hash to have empty tracker data on error")
	}
}
