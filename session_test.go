package qbittorrent

import (
	"cmp"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func sessionMethods() []struct {
	name   string
	action string
	call   func(*Client, context.Context) error
} {
	return []struct {
		name   string
		action string
		call   func(*Client, context.Context) error
	}{
		{"pause", "transfer/pauseSession", (*Client).PauseSessionCtx},
		{"resume", "transfer/resumeSession", (*Client).ResumeSessionCtx},
	}
}

func TestClient_SessionActions(t *testing.T) {
	for _, method := range sessionMethods() {
		for _, tt := range []struct {
			name    string
			apiVer  string
			status  int
			wantErr error
		}{
			{"ok", "", http.StatusOK, nil},
			{"newer server", "2.17.0", http.StatusOK, nil},
			{"no content", "", http.StatusNoContent, nil},
			{"not found", "", http.StatusNotFound, ErrUnexpectedStatus},
			{"conflict", "", http.StatusConflict, ErrUnexpectedStatus},
			{"old server", "2.16.1", 0, ErrUnsupportedVersion},
		} {
			t.Run(method.name+"/"+tt.name, func(t *testing.T) {
				calls := 0
				mux := http.NewServeMux()
				mux.HandleFunc("GET /api/v2/app/webapiVersion", func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte(cmp.Or(tt.apiVer, "2.16.2")))
				})
				mux.HandleFunc("/api/v2/transfer/", func(w http.ResponseWriter, r *http.Request) {
					calls++
					if r.Method != http.MethodPost || r.URL.Path != "/api/v2/"+method.action {
						t.Errorf("request = %s %s, want POST /api/v2/%s", r.Method, r.URL.Path, method.action)
					}
					if body, _ := io.ReadAll(r.Body); len(body) != 0 || r.URL.RawQuery != "" {
						t.Errorf("request sent body %q and query %q, want neither", body, r.URL.RawQuery)
					}
					w.WriteHeader(tt.status)
				})
				server := httptest.NewServer(mux)
				defer server.Close()
				client := NewClient(Config{Host: server.URL, RetryAttempts: 1})

				err := method.call(client, t.Context())
				require.ErrorIs(t, err, tt.wantErr)
				wantCalls := 1
				if tt.status == 0 {
					wantCalls = 0
				}
				require.Equal(t, wantCalls, calls)
			})
		}
	}
}

func TestClient_SessionActions_Canceled(t *testing.T) {
	for _, method := range sessionMethods() {
		t.Run(method.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				t.Errorf("canceled request reached the server: %s", r.URL.Path)
			}))
			defer server.Close()
			client := NewClient(Config{Host: server.URL, RetryAttempts: 1})
			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			require.ErrorIs(t, method.call(client, ctx), context.Canceled)
		})
	}
}

func TestClient_SessionActions_ServerError(t *testing.T) {
	for _, method := range sessionMethods() {
		t.Run(method.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("GET /api/v2/app/webapiVersion", func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("2.16.2"))
			})
			mux.HandleFunc("POST /api/v2/"+method.action, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			})
			server := httptest.NewServer(mux)
			defer server.Close()
			client := NewClient(Config{Host: server.URL, RetryAttempts: 1})

			require.ErrorContains(t, method.call(client, t.Context()), "status: 500")
		})
	}
}

func TestSyncManager_SessionPaused(t *testing.T) {
	read := func(name string) string {
		body, err := os.ReadFile(filepath.Join("testdata", "torrents", name))
		require.NoError(t, err)
		return string(body)
	}
	steps := []struct {
		name        string
		body        string
		want        bool
		serverState bool
	}{
		{"recorded full update", read("5.3.0-full.json"), false, true},
		{"paused", `{"rid":3,"server_state":{"session_state":true}}`, true, true},
		{"recorded delta without the field", read("5.3.0-delta.json"), true, true},
		{"no server state", `{"rid":5}`, true, false},
		{"resumed", `{"rid":6,"server_state":{"session_state":false}}`, false, true},
	}
	call := 0
	sm := newSyncManagerWithTransport(func(*http.Request) (*http.Response, error) {
		body := steps[call].body
		call++
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	var updates []SyncUpdate
	sm.options.OnSyncUpdate = func(update SyncUpdate) { updates = append(updates, update) }

	for _, step := range steps {
		updates = nil
		require.NoError(t, sm.Sync(t.Context()), step.name)
		require.Equal(t, step.want, sm.GetServerStateUnchecked().SessionPaused, step.name)
		if !step.serverState {
			require.Empty(t, updates, step.name)
			continue
		}
		require.Len(t, updates, 1, step.name)
		require.True(t, updates[0].Changes.ServerState, step.name)
		require.Equal(t, step.want, updates[0].ServerState.SessionPaused, step.name)
	}
}

func TestMainData_SessionPausedBeforeWebAPI2_16_2(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "torrents", "5.2.4-full.json"))
	require.NoError(t, err)
	var data MainData
	require.NoError(t, json.Unmarshal(body, &data))
	require.False(t, data.ServerState.SessionPaused)
}
