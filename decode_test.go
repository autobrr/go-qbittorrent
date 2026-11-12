package qbittorrent

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetTrackersCtx(t *testing.T) {
	var query string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v2/torrents/info", func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		trackers := ""
		// qBittorrent adds the trackers only on request.
		if r.URL.Query().Get("includeTrackers") == "true" {
			trackers = `,"trackers":[{"url":"https://tracker.test/announce","status":2,"num_peers":3,"msg":"ok"}]`
		}
		_, _ = fmt.Fprintf(w, `[{"hash":"abc","name":"first","size":4096,"tags":"x, y"%s},{"hash":"def","name":"second"%s}]`, trackers, trackers)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	client := NewClient(Config{Host: server.URL, APIKey: "test-key"})

	got, err := client.GetTrackersCtx(t.Context(), TorrentFilterOptions{Hashes: []string{"abc", "def"}})
	require.NoError(t, err)

	tracker := []TorrentTracker{{Url: "https://tracker.test/announce", Status: TrackerStatusOK, NumPeers: 3, Message: "ok"}}
	require.Equal(t, []TorrentTrackers{{Hash: "abc", Trackers: tracker}, {Hash: "def", Trackers: tracker}}, got)
	require.Equal(t, "hashes=abc%7Cdef&includeTrackers=true", query)
}

func TestSyncMainDataCtxWithRaw_RawDataOnlyForAnUpdate(t *testing.T) {
	first := map[string]Torrent{"abc": {Hash: "abc", AddedOn: 1700000000, Name: "first", Progress: 0.5}}
	tests := []struct {
		name    string
		rid     int64
		body    string
		wantRaw map[string]any
		want    map[string]Torrent
	}{
		{name: "full update for rid 0", rid: 0, body: fullUpdateOneTorrent, want: first},
		{name: "full update for an update request", rid: 4, body: fullUpdateOneTorrent, want: first},
		{
			name:    "update",
			rid:     5,
			body:    `{"rid":6,"torrents":{"abc":{"progress":1}}}`,
			wantRaw: map[string]any{"rid": float64(6), "torrents": map[string]any{"abc": map[string]any{"hash": "abc", "progress": float64(1)}}},
			want:    map[string]Torrent{"abc": {Hash: "abc", Progress: 1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script := &syncScript{replies: []func(http.ResponseWriter, *http.Request){sendBody(tt.body)}}
			server := newScriptedSyncServer(t, "2.16.0", script)
			client := NewClient(Config{Host: server.URL, APIKey: "test-key"})

			data, raw, err := client.SyncMainDataCtxWithRaw(t.Context(), tt.rid)
			require.NoError(t, err)
			require.Equal(t, tt.wantRaw, raw)
			require.Equal(t, tt.want, data.Torrents)
			require.Equal(t, tt.wantRaw == nil, data.FullUpdate)
		})
	}
}

// The raw data tells which fields an update holds. A sync with rid 0 reads
// none, so it cannot apply an update.
func TestSyncMainDataCtxWithRaw_UpdateInAnswerToRid0(t *testing.T) {
	script := &syncScript{replies: []func(http.ResponseWriter, *http.Request){
		sendBody(`{"rid":6,"torrents":{"abc":{"progress":1}}}`),
	}}
	server := newScriptedSyncServer(t, "2.16.0", script)
	client := NewClient(Config{Host: server.URL, APIKey: "test-key"})

	_, _, err := client.SyncMainDataCtxWithRaw(t.Context(), 0)
	require.ErrorContains(t, err, "not a full update")
}

// SyncMainDataCtx returns no raw data, so it must not build any. Only the
// allocations show that.
func TestSyncMainDataCtx_BuildsNoRawData(t *testing.T) {
	const torrents = 1000
	updates := make([]string, torrents)
	for i := range updates {
		updates[i] = fmt.Sprintf(`"hash%d":{"dlspeed":%d,"upspeed":%d}`, i, i, i)
	}
	script := &syncScript{replies: []func(http.ResponseWriter, *http.Request){
		sendBody(`{"rid":6,"torrents":{` + strings.Join(updates, ",") + `}}`),
	}}
	server := newScriptedSyncServer(t, "2.16.0", script)
	client := NewClient(Config{Host: server.URL, APIKey: "test-key"})

	plain := testing.AllocsPerRun(5, func() {
		_, err := client.SyncMainDataCtx(t.Context(), 5)
		require.NoError(t, err)
	})
	withRaw := testing.AllocsPerRun(5, func() {
		_, _, err := client.SyncMainDataCtxWithRaw(t.Context(), 5)
		require.NoError(t, err)
	})

	// The raw data has one map for each torrent.
	require.Less(t, plain+torrents, withRaw)
}
