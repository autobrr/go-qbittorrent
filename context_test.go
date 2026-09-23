package qbittorrent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const syncBody = `{"rid":1,"full_update":true,"torrents":{"abc":{"hash":"abc"}},"categories":{},"tags":[],"server_state":{}}`

// newHangingServer answers sync/maindata with syncBody for the first answered
// requests and makes every later request hang until the client gives up or
// release is closed. It returns the number of requests the server received.
func newHangingServer(t *testing.T, answered int32, release <-chan struct{}) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) > answered {
			select {
			case <-r.Context().Done():
				return
			case <-release:
			}
		}
		_, _ = w.Write([]byte(syncBody))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func newTestClient(host string, attempts int) *Client {
	return NewClient(Config{Host: host, APIKey: "test-key", RetryAttempts: attempts})
}

func TestNewClient_DefaultRetryDelayIsOneSecond(t *testing.T) {
	if got := NewClient(Config{Host: "http://qbit.test"}).retryDelay; got != time.Second {
		t.Fatalf("default retryDelay = %v, want 1s", got)
	}
}

func TestRetryDo_StopsWhenContextEnds(t *testing.T) {
	srv, hits := newHangingServer(t, 0, nil)
	c := newTestClient(srv.URL, 5)

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := c.getCtx(ctx, "sync/maindata", nil)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("retryDo returned after %v, want about 50ms", elapsed)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("server got %d requests, want 1", got)
	}
}

func TestRetryDo_RetriesClientTimeoutWithRetryDelay(t *testing.T) {
	srv, hits := newHangingServer(t, 0, nil)
	c := NewClient(Config{Host: srv.URL, APIKey: "test-key", RetryAttempts: 2, RetryDelay: 1})
	c.http.Timeout = 50 * time.Millisecond

	start := time.Now()
	if _, err := c.getCtx(t.Context(), "sync/maindata", nil); err == nil {
		t.Fatal("expected an error")
	}
	elapsed := time.Since(start)

	if got := hits.Load(); got != 2 {
		t.Fatalf("server got %d requests, want 2", got)
	}
	if elapsed < time.Second {
		t.Fatalf("retryDo returned after %v, want at least the 1s RetryDelay", elapsed)
	}
}

func TestSyncManager_SyncCallerReturnsAtDeadlineWhileSharedSyncContinues(t *testing.T) {
	release := make(chan struct{})
	srv, hits := newHangingServer(t, 0, release)
	sm := NewSyncManager(newTestClient(srv.URL, 1))

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	// The short-deadline caller starts the shared sync.
	shortErr := make(chan error, 1)
	start := time.Now()
	go func() { shortErr <- sm.Sync(ctx) }()
	for hits.Load() == 0 {
		time.Sleep(time.Millisecond)
	}

	// A second caller joins the same sync with no deadline.
	longErr := make(chan error, 1)
	go func() { longErr <- sm.Sync(context.Background()) }()

	if err := <-shortErr; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("short caller: expected context.DeadlineExceeded, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("short caller returned after %v, want about 50ms", elapsed)
	}

	// The deadline of the first caller must not cancel the shared sync.
	close(release)
	select {
	case err := <-longErr:
		if err != nil {
			t.Fatalf("joined caller: expected the shared sync to succeed, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("joined caller did not return")
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("server got %d requests, want 1 shared sync", got)
	}
}

func TestSyncManager_SyncWithDoneContextStartsNoSync(t *testing.T) {
	srv, hits := newHangingServer(t, 1, nil)
	sm := NewSyncManager(newTestClient(srv.URL, 1))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := sm.Sync(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if err := sm.Sync(t.Context()); err != nil {
		t.Fatalf("follow-up sync: %v", err)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("server got %d requests, want 1 (only the follow-up sync)", got)
	}
}

func TestSyncManager_CheckedGetterStaleDataDoesNotBlock(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	srv, hits := newHangingServer(t, 1, release)
	opts := DefaultSyncOptions()
	opts.DynamicSync = true
	sm := NewSyncManager(newTestClient(srv.URL, 5), opts)

	if err := sm.Sync(t.Context()); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	// Make the cached data stale.
	sm.mu.Lock()
	sm.lastSync = time.Now().Add(-time.Hour)
	sm.mu.Unlock()

	start := time.Now()
	torrents := sm.GetTorrents(TorrentFilterOptions{})
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("GetTorrents blocked for %v on stale data", elapsed)
	}
	if len(torrents) != 1 {
		t.Fatalf("expected the cached torrent, got %d", len(torrents))
	}

	// The getter still starts a background sync.
	deadline := time.Now().Add(2 * time.Second)
	for hits.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("server got %d requests, want 2 (initial and background sync)", got)
	}
}

func TestSyncManager_CheckedGetterColdCacheWaitsAtMostOneTimeout(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	srv, _ := newHangingServer(t, 0, release)
	opts := DefaultSyncOptions()
	opts.DynamicSync = true
	c := newTestClient(srv.URL, 5)
	c.timeout = 100 * time.Millisecond
	sm := NewSyncManager(c, opts)

	start := time.Now()
	_ = sm.GetTorrents(TorrentFilterOptions{})
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("GetTorrents blocked for %v on a cold cache, want about 100ms", elapsed)
	}
}
