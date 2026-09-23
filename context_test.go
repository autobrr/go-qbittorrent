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

// waitForHits waits until the server has received n requests.
func waitForHits(t *testing.T, hits *atomic.Int32, n int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for hits.Load() < n {
		if time.Now().After(deadline) {
			t.Fatalf("server got %d requests, want %d", hits.Load(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

// newStaleSyncManager returns a sync manager with dynamic sync on and one
// successful sync whose data is now stale.
func newStaleSyncManager(t *testing.T, c *Client) *SyncManager {
	t.Helper()
	opts := DefaultSyncOptions()
	opts.DynamicSync = true
	sm := NewSyncManager(c, opts)
	if err := sm.Sync(t.Context()); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	sm.mu.Lock()
	sm.lastSync = time.Now().Add(-time.Hour)
	sm.mu.Unlock()
	return sm
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
	c := newTestClient(srv.URL, 2)
	c.http.Timeout = 50 * time.Millisecond
	c.retryDelay = 200 * time.Millisecond

	start := time.Now()
	if _, err := c.getCtx(t.Context(), "sync/maindata", nil); err == nil {
		t.Fatal("expected an error")
	}
	elapsed := time.Since(start)

	if got := hits.Load(); got != 2 {
		t.Fatalf("server got %d requests, want 2", got)
	}
	if elapsed < 250*time.Millisecond {
		t.Fatalf("retryDo returned after %v, want at least one 50ms attempt and the 200ms retry delay", elapsed)
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
	waitForHits(t, hits, 1)

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
	sm := newStaleSyncManager(t, newTestClient(srv.URL, 5))

	start := time.Now()
	torrents := sm.GetTorrents(TorrentFilterOptions{})
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("GetTorrents blocked for %v on stale data", elapsed)
	}
	if len(torrents) != 1 {
		t.Fatalf("expected the cached torrent, got %d", len(torrents))
	}

	// The getter still starts a background sync.
	waitForHits(t, hits, 2)
}

func TestSyncManager_CheckedGetterColdCacheWaitsAtMostOneTimeout(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	srv, _ := newHangingServer(t, 0, release)
	opts := DefaultSyncOptions()
	opts.DynamicSync = true
	c := newTestClient(srv.URL, 5)
	c.http.Timeout = 100 * time.Millisecond // shorter than c.timeout, like a custom client
	sm := NewSyncManager(c, opts)

	start := time.Now()
	_ = sm.GetTorrents(TorrentFilterOptions{})
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("GetTorrents blocked for %v on a cold cache, want about 100ms", elapsed)
	}
}

func TestRetryDo_DialFailureWaitsRetryDelay(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close() // nothing listens here now, so every dial is refused
	c := newTestClient(srv.URL, 2)
	c.retryDelay = 200 * time.Millisecond

	start := time.Now()
	if _, err := c.getCtx(t.Context(), "sync/maindata", nil); err == nil {
		t.Fatal("expected an error")
	}
	if elapsed := time.Since(start); elapsed < c.retryDelay {
		t.Fatalf("retryDo returned after %v, want at least the %v retry delay", elapsed, c.retryDelay)
	}
}

func TestRetryDo_StopsWhenContextEndsDuringRetryDelay(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close() // every dial is refused, so retryDo waits the retry delay
	c := newTestClient(srv.URL, 2)
	c.retryDelay = 2 * time.Second

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := c.getCtx(ctx, "sync/maindata", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("retryDo returned after %v, want about 50ms", elapsed)
	}
}

func TestRetryDo_DoesNotReplayTimedOutPost(t *testing.T) {
	// The server does not see the client go away while the POST body is
	// unread, so release the handler before srv.Close waits for it.
	release := make(chan struct{})
	srv, hits := newHangingServer(t, 0, release)
	t.Cleanup(func() { close(release) })
	c := newTestClient(srv.URL, 5)
	c.http.Timeout = 50 * time.Millisecond

	if _, err := c.postCtx(t.Context(), "torrents/toggleSequentialDownload", nil); err == nil {
		t.Fatal("expected an error")
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("server got %d requests, want 1", got)
	}
}

func TestRetryDo_DoesNotReplayPostAfterConnectionReset(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		conn, _, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		conn.Close() // the server got the request, then the connection drops
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv.URL, 5)

	if _, err := c.postCtx(t.Context(), "torrents/toggleSequentialDownload", nil); err == nil {
		t.Fatal("expected an error")
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("server got %d requests, want 1", got)
	}
}

func TestSyncManager_SharedSyncEndsWithoutClientTimeout(t *testing.T) {
	srv, _ := newHangingServer(t, 0, nil)
	c := newTestClient(srv.URL, 1)
	c.http.Timeout = 0 // a custom http.Client with no timeout
	c.timeout = 50 * time.Millisecond
	c.retryDelay = 0
	sm := NewSyncManager(c)

	start := time.Now()
	if err := sm.Sync(t.Context()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("shared sync ran for %v, want about 50ms", elapsed)
	}
}

func TestSyncManager_SharedSyncBudgetsLogin(t *testing.T) {
	// Login and sync each take most of the attempt timeout, so together they
	// run longer than one attempt timeout.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(70 * time.Millisecond)
		if r.URL.Path == "/api/v2/auth/login" {
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "test", Path: "/"})
			_, _ = w.Write([]byte("Ok."))
			return
		}
		_, _ = w.Write([]byte(syncBody))
	}))
	t.Cleanup(srv.Close)
	c := NewClient(Config{Host: srv.URL, Username: "user", Password: "pass", RetryAttempts: 1})
	c.http.Timeout = 100 * time.Millisecond
	c.retryDelay = 0
	sm := NewSyncManager(c)

	if err := sm.Sync(t.Context()); err != nil {
		t.Fatalf("expected login and sync to fit the shared sync deadline, got %v", err)
	}
}

func TestSyncManager_StaleReadsDoNotJoinRunningSync(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	srv, hits := newHangingServer(t, 1, release)
	sm := newStaleSyncManager(t, newTestClient(srv.URL, 1))

	// The first stale read starts the background sync, which then hangs.
	sm.GetTorrents(TorrentFilterOptions{})
	waitForHits(t, hits, 2)

	// A join sends no request and does not block. Its only trace is the
	// result channel it allocates, so compare against the same read with
	// no sync to join.
	read := func() { sm.GetTorrents(TorrentFilterOptions{}) }
	stale := testing.AllocsPerRun(100, read)
	sm.mu.Lock()
	sm.options.DynamicSync = false
	sm.mu.Unlock()
	if noSync := testing.AllocsPerRun(100, read); stale > noSync {
		t.Fatalf("stale read allocates %v, want at most %v: it joins the running sync", stale, noSync)
	}
}
