package qbittorrent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// newSyncedManager returns a sync manager after one sync of a qBittorrent
// fake that holds torrents.
func newSyncedManager(t testing.TB, torrents ...Torrent) *SyncManager {
	t.Helper()
	byHash := make(map[string]Torrent, len(torrents))
	for _, torrent := range torrents {
		byHash[torrent.Hash] = torrent
	}
	body, err := json.Marshal(map[string]any{"rid": 1, "full_update": true, "torrents": byHash})
	require.NoError(t, err)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/sync/maindata", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	sm := NewSyncManager(NewClient(Config{Host: server.URL, RetryAttempts: 1}))
	require.NoError(t, sm.Sync(t.Context()))
	return sm
}

func hashes(torrents []Torrent) []string {
	out := make([]string, 0, len(torrents))
	for _, torrent := range torrents {
		out = append(out, torrent.Hash)
	}
	slices.Sort(out)
	return out
}

func TestGetTorrentsMatching(t *testing.T) {
	sm := newSyncedManager(t,
		Torrent{Hash: "a", Category: "movies", State: TorrentStateUploading},
		Torrent{Hash: "b", Category: "tv", State: TorrentStateStalledUp},
		Torrent{Hash: "c", Category: "movies", State: TorrentStateStoppedDl},
	)
	movies := func(torrent *Torrent) bool { return torrent.Category == "movies" }

	require.Equal(t, []string{"a", "c"}, hashes(sm.GetTorrentsMatching(movies)))
	require.Equal(t, []string{"a", "c"}, hashes(sm.GetTorrentsMatchingUnchecked(movies)))

	got := sm.GetTorrentsMatchingUnchecked(movies)
	got[0].Category = "changed"
	require.Equal(t, []string{"a", "c"}, hashes(sm.GetTorrentsMatchingUnchecked(movies)),
		"a change to a returned torrent must not reach the cache")
}

func TestGetTorrentsMatchingUncheckedEmptyAndNil(t *testing.T) {
	none := func(*Torrent) bool { return false }

	sm := newSyncedManager(t, Torrent{Hash: "a"})
	got := sm.GetTorrentsMatchingUnchecked(none)
	require.NotNil(t, got, "no match gives an empty slice")
	require.Empty(t, got)

	empty := NewSyncManager(NewClient(Config{Host: "http://127.0.0.1:1"}))
	require.Nil(t, empty.GetTorrentsMatchingUnchecked(func(*Torrent) bool { return true }),
		"a manager with no data gives nil")
}
