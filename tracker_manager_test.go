package qbittorrent

import (
	"context"
	"testing"
	"time"

	"github.com/Masterminds/semver"
)

type mockTrackerAPI struct{}

func (m *mockTrackerAPI) GetTorrentsCtx(ctx context.Context, o TorrentFilterOptions) ([]Torrent, error) {
	return nil, nil
}

func (m *mockTrackerAPI) getApiVersion() (*semver.Version, error) {
	return semver.MustParse("2.11.4"), nil
}

func (m *mockTrackerAPI) GetTorrentTrackersCtx(ctx context.Context, hash string) ([]TorrentTracker, error) {
	return nil, nil
}

func TestTrackerManagerUpdateFromSync(t *testing.T) {
	api := &mockTrackerAPI{}
	manager := NewTrackerManager(api)

	data := map[string][]TorrentTracker{
		"hashA": {
			{Url: "udp://one", Status: TrackerStatusOK},
		},
		"hashB": {
			{Url: "udp://two", Status: TrackerStatusNotWorking},
		},
	}

	// Populate cache with data as if from sync
	mainData := &MainData{
		Torrents: map[string]Torrent{
			"hashA": {Hash: "hashA", Trackers: data["hashA"]},
			"hashB": {Hash: "hashB", Trackers: data["hashB"]},
		},
	}
	manager.UpdateFromSync(mainData)

	torrents := []Torrent{{Hash: "hashA"}, {Hash: "hashB"}}

	enriched, trackerMap := manager.HydrateTorrents(context.Background(), torrents)
	if len(enriched) != 2 {
		t.Fatalf("expected 2 torrents, got %d", len(enriched))
	}

	if len(trackerMap) != 2 {
		t.Fatalf("expected 2 tracker entries, got %d", len(trackerMap))
	}

	if enriched[0].Trackers[0].Url != "udp://one" {
		t.Fatalf("expected tracker URL 'udp://one', got %s", enriched[0].Trackers[0].Url)
	}

	if enriched[1].Trackers[0].Url != "udp://two" {
		t.Fatalf("expected tracker URL 'udp://two', got %s", enriched[1].Trackers[0].Url)
	}
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

func TestTrackerManagerTTLFromReannounce(t *testing.T) {
	api := &mockTrackerAPI{}
	manager := NewTrackerManager(api)

	// Set reannounce to 5 minutes from now
	reannounceTime := time.Now().Add(5 * time.Minute).Unix()

	mainData := &MainData{
		Torrents: map[string]Torrent{
			"hashA": {
				Hash:       "hashA",
				Reannounce: reannounceTime,
				Trackers:   []TorrentTracker{{Url: "udp://test"}},
			},
		},
	}
	manager.UpdateFromSync(mainData)

	// Check that data is cached
	torrents := []Torrent{{Hash: "hashA"}}
	enriched, _ := manager.HydrateTorrents(context.Background(), torrents)
	if len(enriched[0].Trackers) == 0 {
		t.Fatalf("expected tracker data to be cached")
	}
}
