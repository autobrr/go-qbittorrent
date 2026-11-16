package qbittorrent

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
)

func requireInvalidAPIKey(t *testing.T, err error, wantStatus int) {
	t.Helper()

	if !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("err = %v, want ErrInvalidAPIKey", err)
	}
	keyErr, ok := errors.AsType[*InvalidAPIKeyError](err)
	if !ok {
		t.Fatalf("err = %v, want an *InvalidAPIKeyError", err)
	}
	if keyErr.StatusCode != wantStatus {
		t.Errorf("StatusCode = %d, want %d", keyErr.StatusCode, wantStatus)
	}
	if want := fmt.Sprintf("qBittorrent rejected the API key (HTTP %d)", wantStatus); !strings.Contains(err.Error(), want) {
		t.Errorf("err = %q, want it to contain %q", err, want)
	}
}

func forbidden(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "Forbidden", http.StatusForbidden)
}

func TestInvalidAPIKey_ForbiddenOnWebAPIVersion(t *testing.T) {
	client := newCompatibilityClient(forbidden)

	_, err := client.GetWebAPIVersionCtx(t.Context())

	requireInvalidAPIKey(t, err, http.StatusForbidden)
}

func TestInvalidAPIKey_ForbiddenOnAppVersion(t *testing.T) {
	client := newCompatibilityClient(forbidden)

	_, err := client.GetAppVersionCtx(t.Context())

	requireInvalidAPIKey(t, err, http.StatusForbidden)
}

func TestInvalidAPIKey_ForbiddenOnTorrentTrackers(t *testing.T) {
	client := newCompatibilityClient(forbidden)

	trackers, err := client.GetTorrentTrackersCtx(t.Context(), "0123456789abcdef0123456789abcdef01234567")

	requireInvalidAPIKey(t, err, http.StatusForbidden)
	if trackers != nil {
		t.Errorf("trackers = %v, want nil", trackers)
	}
}

func TestInvalidAPIKey_ReannounceStopsAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var requests atomic.Int32
		client := newCompatibilityClient(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			if r.URL.Path != "/api/v2/torrents/trackers" {
				t.Errorf("request to %s, want only torrents/trackers", r.URL.Path)
			}
			forbidden(w, r)
		})

		err := client.ReannounceTorrentWithRetry(t.Context(), "0123456789abcdef0123456789abcdef01234567", &ReannounceOptions{
			Interval:    1,
			MaxAttempts: 3,
		})

		requireInvalidAPIKey(t, err, http.StatusForbidden)
		if got := requests.Load(); got != 1 {
			t.Errorf("requests = %d, want 1", got)
		}
	})
}

func TestInvalidAPIKey_Unauthorized(t *testing.T) {
	tests := []struct {
		name      string
		challenge string
		wantKey   bool
	}{
		{name: "bearer challenge", challenge: `Bearer realm="qBittorrent", error="invalid_token"`, wantKey: true},
		{name: "lowercase scheme", challenge: `bearer realm="qBittorrent", error="invalid_token"`, wantKey: true},
		{name: "no challenge"},
		{name: "basic challenge from a proxy", challenge: `Basic realm="proxy"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A real server, because only the client's response parser makes the
			// lowercase header name canonical.
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				if tt.challenge != "" {
					// qBittorrent 5.4 writes the header name in lowercase.
					w.Header()["www-authenticate"] = []string{tt.challenge}
				}
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
			}))
			defer server.Close()
			client := NewClient(Config{Host: server.URL, APIKey: "fixture"})

			_, err := client.GetTorrentsCtx(t.Context(), TorrentFilterOptions{})

			if got := requests.Load(); got != 1 {
				t.Errorf("requests = %d, want 1", got)
			}
			if tt.wantKey {
				requireInvalidAPIKey(t, err, http.StatusUnauthorized)
				return
			}
			if errors.Is(err, ErrInvalidAPIKey) {
				t.Fatalf("err = %v, want no ErrInvalidAPIKey", err)
			}
			if !errors.Is(err, ErrUnexpectedStatus) || !strings.Contains(err.Error(), "status code: 401") {
				t.Errorf("err = %v, want ErrUnexpectedStatus with status code: 401", err)
			}
		})
	}
}

func TestInvalidAPIKey_ForbiddenOnSetLocationKeepsItsMeaning(t *testing.T) {
	client := newCompatibilityClient(forbidden)

	err := client.SetLocationCtx(t.Context(), []string{"0123456789abcdef0123456789abcdef01234567"}, "/downloads")

	if !errors.Is(err, ErrNoWriteAccessToPath) {
		t.Errorf("err = %v, want ErrNoWriteAccessToPath", err)
	}
	if errors.Is(err, ErrInvalidAPIKey) {
		t.Errorf("err = %v, want no ErrInvalidAPIKey", err)
	}
}

func TestInvalidAPIKey_NotWithoutAPIKey(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var requests atomic.Int32
		client := NewClient(Config{Host: "http://qbittorrent.test"}).WithHTTPClient(&http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				requests.Add(1)
				recorder := httptest.NewRecorder()
				forbidden(recorder, req)
				return recorder.Result(), nil
			}),
		})

		_, err := client.GetWebAPIVersionCtx(t.Context())

		if errors.Is(err, ErrInvalidAPIKey) {
			t.Fatalf("err = %v, want no ErrInvalidAPIKey", err)
		}
		if err == nil || !strings.Contains(err.Error(), "qbit re-login") {
			t.Errorf("err = %v, want the qbit re-login error", err)
		}
		if got := requests.Load(); got != 5 {
			t.Errorf("requests = %d, want 5", got)
		}
	})
}

// The transport sends a sync request again by itself when a reused connection
// closes. The Bearer 401 on that resend must not start the lost-response replay.
func TestInvalidAPIKey_TransportResendOfSync(t *testing.T) {
	script := &syncScript{replies: []func(http.ResponseWriter, *http.Request){
		sendBody(fullUpdateOneTorrent),
		closeConnection,
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="qBittorrent", error="invalid_token"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		},
	}}
	server := newScriptedSyncServer(t, "2.15.1", script)
	client := newLostAttemptClient(t, server)
	if _, err := client.GetWebAPIVersionCtx(t.Context()); err != nil {
		t.Fatalf("read the version: %v", err)
	}
	sm := NewSyncManager(client)
	if err := sm.Sync(t.Context()); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	err := sm.Sync(t.Context())

	requireInvalidAPIKey(t, err, http.StatusUnauthorized)
	if want, got := []string{"0", "5", "5"}, script.seenRids(); !slices.Equal(got, want) {
		t.Errorf("rids = %v, want %v", got, want)
	}
}

func TestInvalidAPIKey_UnauthorizedWithoutAPIKey(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("WWW-Authenticate", `Bearer realm="qBittorrent", error="invalid_token"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()
	client := NewClient(Config{Host: server.URL})

	_, err := client.GetTorrentsCtx(t.Context(), TorrentFilterOptions{})

	if errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("err = %v, want no ErrInvalidAPIKey", err)
	}
	if !errors.Is(err, ErrUnexpectedStatus) || !strings.Contains(err.Error(), "status code: 401") {
		t.Errorf("err = %v, want ErrUnexpectedStatus with status code: 401", err)
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("requests = %d, want 1", got)
	}
}
