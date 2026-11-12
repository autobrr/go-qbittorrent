package qbittorrent

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestGetTrackersCtx_RecordedResponses(t *testing.T) {
	for _, version := range []string{"4.6.7", "5.0.0", "5.1.4", "5.2.4", "5.3.0"} {
		t.Run(version, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("testdata", "torrents", version+"-info-trackers.json"))
			require.NoError(t, err)
			client := newCompatibilityClient(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(body)
			})

			torrents, err := client.GetTorrentsCtx(t.Context(), TorrentFilterOptions{IncludeTrackers: true})
			require.NoError(t, err)
			require.NotEmpty(t, torrents)
			want := make([]TorrentTrackers, len(torrents))
			for i, torrent := range torrents {
				require.NotEmpty(t, torrent.Hash)
				want[i] = TorrentTrackers{Hash: torrent.Hash, Trackers: torrent.Trackers}
			}

			got, err := client.GetTrackersCtx(t.Context(), TorrentFilterOptions{})
			require.NoError(t, err)
			require.Equal(t, want, got)
		})
	}
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

// The read returns nil raw data for a full update, so it must not build any.
// Only the allocations show that.
func TestSyncMainDataCtxWithRaw_BuildsNoRawDataForAFullUpdate(t *testing.T) {
	const torrents = 1000
	body := newSyncLibrary(torrents, 0).full
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	client := NewClient(Config{Host: server.URL, APIKey: "test-key", RetryAttempts: 1})

	plain := testing.AllocsPerRun(5, func() {
		_, err := client.SyncMainDataCtx(t.Context(), 5)
		require.NoError(t, err)
	})
	withRaw := testing.AllocsPerRun(5, func() {
		_, _, err := client.SyncMainDataCtxWithRaw(t.Context(), 5)
		require.NoError(t, err)
	})

	// The raw data has one map for each torrent.
	require.Less(t, withRaw, plain+torrents)
}

// A read that holds the whole response allocates at least the size of that
// response on top of the decoded data.
func TestStreamReads_DoNotHoldTheWholeResponse(t *testing.T) {
	const torrents = 5000
	lib := newSyncLibrary(torrents, 0)
	var full MainData
	require.NoError(t, json.Unmarshal(lib.full, &full))
	list := make([]Torrent, 0, torrents)
	for _, torrent := range full.Torrents {
		torrent.Trackers = []TorrentTracker{{Url: "https://tracker.test/announce", Status: TrackerStatusOK}}
		list = append(list, torrent)
	}
	listBody, err := json.Marshal(list)
	require.NoError(t, err)

	tests := []struct {
		name string
		body []byte
		into func() any
		read func(*Client) error
	}{
		{"full update for rid 0", lib.full, func() any { return new(MainData) }, func(c *Client) error {
			_, _, err := c.SyncMainDataCtxWithRaw(t.Context(), 0)
			return err
		}},
		{"full update without raw data", lib.full, func() any { return new(MainData) }, func(c *Client) error {
			_, err := c.SyncMainDataCtx(t.Context(), 5)
			return err
		}},
		{"tracker read", listBody, func() any { return new([]TorrentTrackers) }, func(c *Client) error {
			_, err := c.GetTrackersCtx(t.Context(), TorrentFilterOptions{})
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(tt.body)
			}))
			t.Cleanup(server.Close)
			client := NewClient(Config{Host: server.URL, APIKey: "test-key", RetryAttempts: 1})

			decoded := bytesPerCall(func() { require.NoError(t, json.Unmarshal(tt.body, tt.into(), streamOptions)) })
			allocated := bytesPerCall(func() { require.NoError(t, tt.read(client)) })
			require.Less(t, allocated, decoded+uint64(len(tt.body))/2)
		})
	}
}
