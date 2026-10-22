package qbittorrent

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

type mockTrackerAPI struct{}

func (m *mockTrackerAPI) GetTorrentsCtx(ctx context.Context, o TorrentFilterOptions) ([]Torrent, error) {
	return nil, nil
}

func TestTrackerManagerHydrateWithExistingTrackers(t *testing.T) {
	api := &mockTrackerAPI{}
	manager := NewTrackerManager(api)

	// Torrent already has trackers
	torrents := []Torrent{
		{Hash: "hashA", Trackers: []TorrentTracker{{Url: "udp://existing"}}},
		{Hash: "hashB"},
	}

	enriched, trackerMap := manager.HydrateTorrents(context.Background(), torrents)
	if len(enriched) != 2 {
		t.Fatalf("expected 2 torrents, got %d", len(enriched))
	}

	if len(trackerMap) != 1 {
		t.Fatalf("expected 1 tracker entry, got %d", len(trackerMap))
	}

	if enriched[0].Trackers[0].Url != "udp://existing" {
		t.Fatalf("expected existing tracker to be preserved")
	}
}

type recordingTrackerAPI struct {
	data      map[string][]TorrentTracker
	calls     []int
	failAbove int
}

func (a *recordingTrackerAPI) GetTorrentsCtx(ctx context.Context, o TorrentFilterOptions) ([]Torrent, error) {
	if !o.IncludeTrackers {
		return nil, nil
	}

	if len(o.Hashes) == 0 {
		a.calls = append(a.calls, 0)
		result := make([]Torrent, 0, len(a.data))
		for hash, trackers := range a.data {
			result = append(result, Torrent{Hash: hash, Trackers: trackers})
		}
		return result, nil
	}

	a.calls = append(a.calls, len(o.Hashes))

	if a.failAbove > 0 && len(o.Hashes) > a.failAbove {
		return nil, errors.New("request too large")
	}

	result := make([]Torrent, 0, len(o.Hashes))
	for _, hash := range o.Hashes {
		trackers := a.data[hash]
		result = append(result, Torrent{Hash: hash, Trackers: trackers})
	}
	return result, nil
}

func TestTrackerManagerHydrateWithIncludeTrackersSingleRequest(t *testing.T) {
	total := trackerIncludeChunkSize*2 + 10
	data := make(map[string][]TorrentTracker, total)
	torrents := make([]Torrent, total)
	for i := 0; i < total; i++ {
		hash := fmt.Sprintf("HASH%04d", i)
		data[hash] = []TorrentTracker{{Url: fmt.Sprintf("udp://tracker/%d", i), Status: TrackerStatusOK}}
		torrents[i] = Torrent{Hash: hash}
	}

	api := &recordingTrackerAPI{data: data}
	manager := NewTrackerManager(api)
	manager.SetUseIncludeTrackers(true)

	enriched, trackerMap := manager.HydrateTorrents(context.Background(), torrents)
	if len(enriched) != total {
		t.Fatalf("expected %d torrents, got %d", total, len(enriched))
	}

	for i, torrent := range enriched {
		if len(torrent.Trackers) != 1 {
			t.Fatalf("expected trackers for torrent %d", i)
		}
	}

	if len(trackerMap) != total {
		t.Fatalf("expected tracker map entries for all torrents, got %d", len(trackerMap))
	}

	if len(api.calls) != 1 {
		t.Fatalf("expected single request, got %d", len(api.calls))
	}

	if api.calls[0] != total {
		t.Fatalf("expected single request to include all hashes, got %d", api.calls[0])
	}
}

func TestTrackerManagerHydrateWithIncludeTrackersChunks(t *testing.T) {
	total := trackerIncludeChunkSize*2 + 10
	data := make(map[string][]TorrentTracker, total)
	torrents := make([]Torrent, total)
	for i := 0; i < total; i++ {
		hash := fmt.Sprintf("HASH%04d", i)
		data[hash] = []TorrentTracker{{Url: fmt.Sprintf("udp://tracker/%d", i), Status: TrackerStatusOK}}
		torrents[i] = Torrent{Hash: hash}
	}

	api := &recordingTrackerAPI{data: data, failAbove: trackerIncludeChunkSize}
	manager := NewTrackerManager(api)
	manager.SetUseIncludeTrackers(true)

	enriched, trackerMap := manager.HydrateTorrents(context.Background(), torrents)
	if len(enriched) != total {
		t.Fatalf("expected %d torrents, got %d", total, len(enriched))
	}

	for i, torrent := range enriched {
		if len(torrent.Trackers) != 1 {
			t.Fatalf("expected trackers for torrent %d", i)
		}
	}

	if len(trackerMap) != total {
		t.Fatalf("expected tracker map entries for all torrents, got %d", len(trackerMap))
	}

	expectedChunks := (total + trackerIncludeChunkSize - 1) / trackerIncludeChunkSize
	if len(api.calls) != expectedChunks+1 {
		t.Fatalf("expected %d calls (1 large + %d chunks), got %d", expectedChunks+1, expectedChunks, len(api.calls))
	}

	if api.calls[0] != total {
		t.Fatalf("expected initial large request with %d hashes, got %d", total, api.calls[0])
	}

	for _, size := range api.calls[1:] {
		if size > trackerIncludeChunkSize {
			t.Fatalf("batch size %d exceeds chunk size %d", size, trackerIncludeChunkSize)
		}
		if size == 0 {
			t.Fatalf("unexpected full-fetch call during chunking test")
		}
	}
}

type fallbackTrackerAPI struct {
	data           map[string][]TorrentTracker
	chunkCalls     int
	fullFetchCalls int
}

func (a *fallbackTrackerAPI) GetTorrentsCtx(ctx context.Context, o TorrentFilterOptions) ([]Torrent, error) {
	if !o.IncludeTrackers {
		return nil, nil
	}

	if len(o.Hashes) == 0 {
		a.fullFetchCalls++
		result := make([]Torrent, 0, len(a.data))
		for hash, trackers := range a.data {
			result = append(result, Torrent{Hash: hash, Trackers: trackers})
		}
		return result, nil
	}

	a.chunkCalls++
	return []Torrent{}, nil
}

func TestTrackerManagerHydrateWithIncludeTrackersFallback(t *testing.T) {
	data := map[string][]TorrentTracker{
		"HASHA": {{Url: "udp://fallback/a", Status: TrackerStatusOK}},
		"HASHB": {{Url: "udp://fallback/b", Status: TrackerStatusOK}},
	}

	api := &fallbackTrackerAPI{data: data}
	manager := NewTrackerManager(api)
	manager.SetUseIncludeTrackers(true)

	torrents := []Torrent{{Hash: "HASHA"}, {Hash: "HASHB"}}

	enriched, trackerMap := manager.HydrateTorrents(context.Background(), torrents)
	if api.chunkCalls == 0 {
		t.Fatalf("expected chunk request before fallback")
	}
	if api.fullFetchCalls != 1 {
		t.Fatalf("expected a single full-fetch fallback, got %d", api.fullFetchCalls)
	}

	for i, torrent := range enriched {
		if len(torrent.Trackers) == 0 {
			t.Fatalf("expected trackers after fallback for torrent %d", i)
		}
	}

	if len(trackerMap) != len(torrents) {
		t.Fatalf("expected tracker map entries for all torrents, got %d", len(trackerMap))
	}
}

type statusTrackerAPI struct {
	status TrackerStatus
	err    error
	calls  int
	last   TorrentFilterOptions
}

func (a *statusTrackerAPI) GetTorrentsCtx(ctx context.Context, o TorrentFilterOptions) ([]Torrent, error) {
	a.calls++
	a.last = o
	if a.err != nil {
		return nil, a.err
	}
	return []Torrent{{Hash: "HASHA", Trackers: []TorrentTracker{{Url: "udp://a", Status: a.status}}}}, nil
}

func TestTrackerManagerRefreshReplacesCachedEntry(t *testing.T) {
	api := &statusTrackerAPI{status: TrackerStatusOK}
	manager := NewTrackerManager(api)
	manager.SetUseIncludeTrackers(true)
	manager.HydrateTorrents(t.Context(), []Torrent{{Hash: "HASHA"}})

	api.status = TrackerStatusNotWorking
	calls := api.calls
	refreshed, trackerMap, err := manager.Refresh(t.Context(), []Torrent{{Hash: "HASHA", Trackers: []TorrentTracker{{Url: "udp://a", Status: TrackerStatusOK}}}})
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	if api.calls-calls != 1 || len(api.last.Hashes) != 0 || !api.last.IncludeTrackers {
		t.Fatalf("Refresh sent %d requests, last %+v; want 1 unfiltered includeTrackers request", api.calls-calls, api.last)
	}
	if got := refreshed[0].Trackers[0].Status; got != TrackerStatusNotWorking {
		t.Fatalf("Refresh returned status %v, want %v", got, TrackerStatusNotWorking)
	}
	if got := trackerMap["HASHA"][0].Status; got != TrackerStatusNotWorking {
		t.Fatalf("Refresh map status %v, want %v", got, TrackerStatusNotWorking)
	}

	calls = api.calls
	hydrated, _ := manager.HydrateTorrents(t.Context(), []Torrent{{Hash: "HASHA"}})
	if api.calls != calls {
		t.Fatalf("HydrateTorrents sent %d requests, want 0", api.calls-calls)
	}
	if got := hydrated[0].Trackers[0].Status; got != TrackerStatusNotWorking {
		t.Fatalf("HydrateTorrents served status %v, want %v", got, TrackerStatusNotWorking)
	}
}

func TestTrackerManagerRefreshKeepsCacheOnError(t *testing.T) {
	api := &statusTrackerAPI{status: TrackerStatusOK}
	manager := NewTrackerManager(api)
	manager.SetUseIncludeTrackers(true)
	manager.HydrateTorrents(t.Context(), []Torrent{{Hash: "HASHA"}})

	api.err = errors.New("timeout")
	calls := api.calls
	refreshed, trackerMap, err := manager.Refresh(t.Context(), []Torrent{{Hash: "HASHA"}})
	if !errors.Is(err, api.err) {
		t.Fatalf("Refresh returned error %v, want %v", err, api.err)
	}
	if api.calls-calls != 1 {
		t.Fatalf("failed Refresh sent %d requests, want 1", api.calls-calls)
	}
	if len(trackerMap) != 0 || len(refreshed[0].Trackers) != 0 {
		t.Fatalf("failed Refresh changed the result: map %v, trackers %+v", trackerMap, refreshed[0].Trackers)
	}

	calls = api.calls
	hydrated, _ := manager.HydrateTorrents(t.Context(), []Torrent{{Hash: "HASHA"}})
	if api.calls != calls {
		t.Fatalf("HydrateTorrents sent %d requests, want 0", api.calls-calls)
	}
	if len(hydrated[0].Trackers) == 0 || hydrated[0].Trackers[0].Status != TrackerStatusOK {
		t.Fatalf("cache entry lost after failed Refresh: %+v", hydrated[0].Trackers)
	}
}

func TestTrackerManagerWithoutIncludeTrackersSendsNoRequest(t *testing.T) {
	api := &statusTrackerAPI{status: TrackerStatusOK}
	manager := NewTrackerManager(api)
	manager.SetUseIncludeTrackers(false)

	torrents := []Torrent{{Hash: "HASHA"}}
	manager.HydrateTorrents(t.Context(), torrents)
	if _, _, err := manager.Refresh(t.Context(), torrents); err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	if api.calls != 0 {
		t.Fatalf("sent %d requests, want 0", api.calls)
	}
	if len(torrents[0].Trackers) != 0 {
		t.Fatalf("torrent changed: %+v", torrents[0].Trackers)
	}
}
