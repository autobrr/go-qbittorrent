package qbittorrent

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
)

func TestClient_ReannounceTorrentWithRetry(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"

	tests := []struct {
		name            string
		statuses        []TrackerStatus // tracker status on each check, the last one repeats
		deleteOnFailure bool
		wantErr         error
		wantChecks      int
		wantReannounces int
		wantDeleted     bool
	}{
		{
			name:            "stalled_delete",
			statuses:        []TrackerStatus{TrackerStatusNotWorking},
			deleteOnFailure: true,
			wantErr:         ErrReannounceTookTooLong,
			wantChecks:      3,
			wantReannounces: 2,
			wantDeleted:     true,
		},
		{
			name:            "stalled_keep",
			statuses:        []TrackerStatus{TrackerStatusNotWorking},
			wantChecks:      3,
			wantReannounces: 2,
		},
		{
			name:            "working_tracker",
			statuses:        []TrackerStatus{TrackerStatusOK},
			deleteOnFailure: true,
			wantChecks:      1,
		},
		{
			name:            "working_after_last_reannounce",
			statuses:        []TrackerStatus{TrackerStatusNotWorking, TrackerStatusNotWorking, TrackerStatusOK},
			deleteOnFailure: true,
			wantChecks:      3,
			wantReannounces: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var (
				mu          sync.Mutex
				checks      int
				reannounced []string
				deleted     []string
			)

			mux := http.NewServeMux()
			mux.HandleFunc("GET /api/v2/torrents/trackers", func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				status := tt.statuses[min(checks, len(tt.statuses)-1)]
				checks++
				mu.Unlock()

				if got := r.URL.Query().Get("hash"); got != hash {
					t.Errorf("trackers hash = %q, want %q", got, hash)
				}

				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `[{"url":"** [DHT] **","status":0,"msg":"This torrent is private"},{"url":"https://tracker.example/announce","status":%d,"msg":""}]`, status)
			})
			mux.HandleFunc("POST /api/v2/torrents/reannounce", func(_ http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				reannounced = append(reannounced, r.FormValue("hashes"))
			})
			mux.HandleFunc("POST /api/v2/torrents/delete", func(_ http.ResponseWriter, r *http.Request) {
				if got := r.FormValue("deleteFiles"); got != "false" {
					t.Errorf("deleteFiles = %q, want false", got)
				}

				mu.Lock()
				defer mu.Unlock()
				deleted = append(deleted, r.FormValue("hashes"))
			})

			server := httptest.NewServer(mux)
			defer server.Close()

			client := NewClient(Config{Host: server.URL, APIKey: "test-key"})

			err := client.ReannounceTorrentWithRetry(t.Context(), hash, &ReannounceOptions{
				Interval:        1,
				MaxAttempts:     2,
				DeleteOnFailure: tt.deleteOnFailure,
			})

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}

			mu.Lock()
			defer mu.Unlock()

			if checks != tt.wantChecks {
				t.Errorf("tracker checks = %d, want %d", checks, tt.wantChecks)
			}
			if len(reannounced) != tt.wantReannounces {
				t.Errorf("re-announces = %d, want %d", len(reannounced), tt.wantReannounces)
			}
			for _, got := range reannounced {
				if got != hash {
					t.Errorf("re-announced hashes = %q, want %q", got, hash)
				}
			}

			var wantDeleted []string
			if tt.wantDeleted {
				wantDeleted = []string{hash}
			}
			if !slices.Equal(deleted, wantDeleted) {
				t.Errorf("deleted = %q, want %q", deleted, wantDeleted)
			}
		})
	}
}
