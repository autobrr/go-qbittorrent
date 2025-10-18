package qbittorrent

import (
	"context"
	"testing"

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
