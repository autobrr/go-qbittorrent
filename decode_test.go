package qbittorrent

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestSyncMainDataCtxWithRaw_RawDataOnlyForAnUpdateRequest(t *testing.T) {
	script := &syncScript{replies: []func(http.ResponseWriter, *http.Request){
		sendBody(fullUpdateOneTorrent),
		sendBody(`{"rid":6,"torrents":{"abc":{"progress":1}}}`),
	}}
	server := newScriptedSyncServer(t, "2.16.0", script)
	client := NewClient(Config{Host: server.URL, APIKey: "test-key"})

	full, raw, err := client.SyncMainDataCtxWithRaw(t.Context(), 0)
	require.NoError(t, err)
	require.Nil(t, raw)
	require.True(t, full.FullUpdate)
	require.Equal(t, map[string]Torrent{"abc": {Hash: "abc", AddedOn: 1700000000, Name: "first", Progress: 0.5}}, full.Torrents)

	update, raw, err := client.SyncMainDataCtxWithRaw(t.Context(), full.Rid)
	require.NoError(t, err)
	require.False(t, update.FullUpdate)
	require.Equal(t, map[string]any{"rid": float64(6), "torrents": map[string]any{"abc": map[string]any{"hash": "abc", "progress": float64(1)}}}, raw)
}

// The raw data tells which fields an update holds. A sync with rid 0 reads
// none, so it cannot apply an update.
func TestSyncMainDataCtx_UpdateInAnswerToRid0(t *testing.T) {
	script := &syncScript{replies: []func(http.ResponseWriter, *http.Request){
		sendBody(`{"rid":6,"torrents":{"abc":{"progress":1}}}`),
	}}
	server := newScriptedSyncServer(t, "2.16.0", script)
	client := NewClient(Config{Host: server.URL, APIKey: "test-key"})

	_, err := client.SyncMainDataCtx(t.Context(), 0)
	require.ErrorContains(t, err, "not a full update")
}

// The two decodes of an update share one pipe. When the typed decode stops at
// an error, the other must not wait for a reader of the rest.
func TestSyncMainDataCtx_DecodeErrorBeforeTheEndOfTheBody(t *testing.T) {
	torrents := make([]string, 4000)
	for i := range torrents {
		torrents[i] = fmt.Sprintf(`"hash%d":{"name":"%s","progress":1}`, i, strings.Repeat("n", 200))
	}
	body := `{"rid":"not a number","torrents":{` + strings.Join(torrents, ",") + `}}`
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v2/sync/maindata", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	client := NewClient(Config{Host: server.URL, APIKey: "test-key"})

	done := make(chan error, 1)
	go func() {
		_, err := client.SyncMainDataCtx(t.Context(), 5)
		done <- err
	}()

	select {
	case err := <-done:
		require.ErrorContains(t, err, "could not unmarshal body")
	case <-time.After(10 * time.Second):
		t.Fatal("the sync did not return after the decode error")
	}
}
