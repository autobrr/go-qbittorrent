package qbittorrent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// qBittorrent 4.x serves torrents/pause; 5.x (WebAPI 2.11) replaced it with
// torrents/stop. An upgrade under a running client must not keep hitting the
// removed endpoint.
func TestPauseCtx_AfterServerUpgrade(t *testing.T) {
	var upgraded atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v2/app/webapiVersion", func(w http.ResponseWriter, _ *http.Request) {
		if upgraded.Load() {
			_, _ = w.Write([]byte("2.11.4"))
			return
		}
		_, _ = w.Write([]byte("2.9.3"))
	})
	mux.HandleFunc("POST /api/v2/torrents/pause", func(w http.ResponseWriter, _ *http.Request) {
		if upgraded.Load() {
			http.NotFound(w, nil)
		}
	})
	mux.HandleFunc("POST /api/v2/torrents/stop", func(w http.ResponseWriter, _ *http.Request) {
		if !upgraded.Load() {
			http.NotFound(w, nil)
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := NewClient(Config{Host: server.URL, RetryAttempts: 1})

	if err := client.PauseCtx(t.Context(), []string{"abc"}); err != nil {
		t.Fatalf("PauseCtx() before upgrade: %v", err)
	}

	upgraded.Store(true)
	// qui polls this on every capability refresh and shows 2.11.4 on the dashboard.
	if v, err := client.GetWebAPIVersionCtx(t.Context()); err != nil || v != "2.11.4" {
		t.Fatalf("GetWebAPIVersionCtx() = %q, %v", v, err)
	}

	if err := client.PauseCtx(t.Context(), []string{"abc"}); err != nil {
		t.Fatalf("PauseCtx() after upgrade: %v", err)
	}
}

// The first version check on a new client sends app/webapiVersion. A hung
// qBittorrent must not hold the caller past its own deadline.
func TestListDirectoryCtx_VersionFetchUsesCallerContext(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v2/app/webapiVersion", func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := NewClient(Config{Host: server.URL, RetryAttempts: 1})

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := client.ListDirectoryCtx(ctx, "/downloads", DirectoryContentAll, false)

	if err == nil {
		t.Fatal("ListDirectoryCtx() error = nil, want the deadline error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("ListDirectoryCtx() returned after %v, want shortly after the 50ms deadline", elapsed)
	}
}
