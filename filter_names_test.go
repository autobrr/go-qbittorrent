package qbittorrent

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The filter names each qBittorrent version accepts in torrents/info come from
// TorrentFilter::setTypeByName in src/base/torrentfilter.cpp (release-4.3.9 to release-5.0.0) and
// parseTorrentStatus in src/webui/api/torrentscontroller.cpp (master). An unknown name matches every
// torrent. Commit 5d1c2496 renamed "paused"/"resumed" to "stopped"/"running" and moved API_VERSION in
// src/webui/webapplication.h from 2.10.4 to 2.11.0; release-4.6.7 serves 2.9.3, release-5.0.0 2.11.2.
func TestGetTorrentsCtx_FilterNames(t *testing.T) {
	older := map[TorrentFilter]string{
		TorrentFilterPaused:    "paused",
		TorrentFilterStopped:   "paused",
		TorrentFilterResumed:   "resumed",
		TorrentFilterRunning:   "resumed",
		TorrentFilterUploading: "seeding",
	}
	modern := map[TorrentFilter]string{
		TorrentFilterPaused:    "stopped",
		TorrentFilterStopped:   "stopped",
		TorrentFilterResumed:   "running",
		TorrentFilterRunning:   "running",
		TorrentFilterUploading: "seeding",
	}

	for _, tc := range []struct {
		webAPIVersion string
		want          map[TorrentFilter]string
	}{
		{"2.9.3", older},  // qBittorrent 4.6.7
		{"2.10.4", older}, // last WebAPI version before the rename
		{"2.11.0", modern},
		{"2.11.2", modern}, // qBittorrent 5.0.0
		{"2.16.2", modern}, // qBittorrent master
	} {
		t.Run(tc.webAPIVersion, func(t *testing.T) {
			var gotFilter string
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v2/app/webapiVersion", func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.webAPIVersion))
			})
			mux.HandleFunc("/api/v2/torrents/info", func(w http.ResponseWriter, r *http.Request) {
				gotFilter = r.URL.Query().Get("filter")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`[]`))
			})
			server := httptest.NewServer(mux)
			defer server.Close()
			client := NewClient(Config{Host: server.URL, RetryAttempts: 1})

			for filter, want := range tc.want {
				gotFilter = ""
				if _, err := client.GetTorrentsCtx(t.Context(), TorrentFilterOptions{Filter: filter}); err != nil {
					t.Fatalf("GetTorrentsCtx(%q): %v", filter, err)
				}
				if gotFilter != want {
					t.Errorf("filter %q sent as %q, want %q", filter, gotFilter, want)
				}
			}
		})
	}
}

func TestParseTorrentFilter(t *testing.T) {
	// Every name qBittorrent accepts in some version from 4.3.9 to master.
	for name, want := range map[string]TorrentFilter{
		"all":                 TorrentFilterAll,
		"downloading":         TorrentFilterDownloading,
		"seeding":             TorrentFilterUploading,
		"completed":           TorrentFilterCompleted,
		"paused":              TorrentFilterPaused,
		"resumed":             TorrentFilterResumed,
		"stopped":             TorrentFilterStopped,
		"running":             TorrentFilterRunning,
		"active":              TorrentFilterActive,
		"inactive":            TorrentFilterInactive,
		"stalled":             TorrentFilterStalled,
		"stalled_uploading":   TorrentFilterStalledUploading,
		"stalled_downloading": TorrentFilterStalledDownloading,
		"checking":            TorrentFilterChecking,
		"moving":              TorrentFilterMoving,
		"errored":             TorrentFilterError,
		// The library's own TorrentFilterUploading value, which qBittorrent calls "seeding".
		"uploading": TorrentFilterUploading,
	} {
		if got := ParseTorrentFilter(name); got != want {
			t.Errorf("ParseTorrentFilter(%q) = %q, want %q", name, got, want)
		}
	}
}
