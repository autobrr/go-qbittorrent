package qbittorrent

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"
)

// syncScript is a fake sync/maindata action. Request n gets replies[n], and each
// request after the last reply gets the last reply again.
type syncScript struct {
	mu      sync.Mutex
	rids    []string
	replies []func(w http.ResponseWriter, r *http.Request)
}

func (s *syncScript) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.rids = append(s.rids, r.URL.Query().Get("rid"))
	reply := s.replies[min(len(s.rids), len(s.replies))-1]
	s.mu.Unlock()
	reply(w, r)
}

func (s *syncScript) seenRids() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.rids)
}

func sendBody(body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	}
}

// newScriptedSyncServer serves script as sync/maindata. An empty webAPIVersion
// leaves the app/webapiVersion action out.
func newScriptedSyncServer(t *testing.T, webAPIVersion string, script *syncScript) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("GET /api/v2/sync/maindata", script)
	if webAPIVersion != "" {
		mux.HandleFunc("GET /api/v2/app/webapiVersion", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, webAPIVersion)
		})
	}
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

const (
	fullUpdateOneTorrent  = `{"rid":5,"full_update":true,"torrents":{"abc":{"added_on":1700000000,"name":"first","progress":0.5}},"categories":{},"tags":[],"server_state":{}}`
	fullUpdateTwoTorrents = `{"rid":1,"full_update":true,"torrents":{"abc":{"added_on":1700000000,"name":"first","progress":1},"def":{"added_on":1700000500,"name":"second","size":4096,"progress":1,"state":"stalledUP"}},"categories":{},"tags":[],"server_state":{}}`
)

// qBittorrent before 5.3.0rc1 can send only the last changed fields of a
// torrent that the client never received. Such a torrent stays out of the
// cache, and the next sync asks for a full update.
func TestSyncManager_DiffForUnknownTorrentWithoutAddedOn(t *testing.T) {
	script := &syncScript{replies: []func(http.ResponseWriter, *http.Request){
		sendBody(fullUpdateOneTorrent),
		sendBody(`{"rid":6,"torrents":{"abc":{"progress":1},"def":{"progress":1,"state":"stalledUP"}},"server_state":{"dl_info_speed":7}}`),
		sendBody(fullUpdateTwoTorrents),
	}}
	server := newScriptedSyncServer(t, "", script)

	var leftOut [][]string
	var updates []MainDataChanges
	options := DefaultSyncOptions()
	options.OnTorrentsLeftOut = func(hashes []string) { leftOut = append(leftOut, hashes) }
	options.OnUpdate = func(data *MainData) { updates = append(updates, data.Changes) }
	sm := NewSyncManager(NewClient(Config{Host: server.URL, APIKey: "test-key", RetryAttempts: 1}), options)

	for i := range 2 {
		if err := sm.Sync(t.Context()); err != nil {
			t.Fatalf("sync %d: %v", i+1, err)
		}
	}

	if _, ok := sm.GetTorrentUnchecked("def"); ok {
		t.Fatal("expected the torrent without added_on to stay out of the cache")
	}
	if len(leftOut) != 1 || !slices.Equal(leftOut[0], []string{"def"}) {
		t.Fatalf("expected one callback with [def], got %v", leftOut)
	}
	if got := updates[1].Torrents; !slices.Equal(got, []string{"abc"}) {
		t.Fatalf("expected only abc as changed, got %v", got)
	}
	if known, _ := sm.GetTorrentUnchecked("abc"); known.Progress != 1 {
		t.Fatalf("expected the rest of the response to apply, got progress %v", known.Progress)
	}
	if got := sm.GetServerStateUnchecked().DlInfoSpeed; got != 7 {
		t.Fatalf("expected the server state to apply, got speed %d", got)
	}

	if err := sm.Sync(t.Context()); err != nil {
		t.Fatalf("sync 3: %v", err)
	}
	if want, got := []string{"0", "5", "0"}, script.seenRids(); !slices.Equal(got, want) {
		t.Fatalf("expected rids %v, got %v", want, got)
	}
	torrent, ok := sm.GetTorrentUnchecked("def")
	if !ok || torrent.Name != "second" || torrent.AddedOn != 1700000500 || torrent.Size != 4096 {
		t.Fatalf("expected the complete torrent after the full update, got %+v", torrent)
	}
	if len(leftOut) != 1 {
		t.Fatalf("expected no callback for the full update, got %v", leftOut)
	}
}

func TestSyncManager_DiffForUnknownTorrentWithAddedOn(t *testing.T) {
	script := &syncScript{replies: []func(http.ResponseWriter, *http.Request){
		sendBody(fullUpdateOneTorrent),
		sendBody(`{"rid":6,"torrents":{"def":{"added_on":1700000500,"name":"second"}}}`),
		sendBody(`{"rid":7}`),
	}}
	server := newScriptedSyncServer(t, "", script)

	called := false
	options := DefaultSyncOptions()
	options.OnTorrentsLeftOut = func([]string) { called = true }
	sm := NewSyncManager(NewClient(Config{Host: server.URL, APIKey: "test-key", RetryAttempts: 1}), options)

	for i := range 3 {
		if err := sm.Sync(t.Context()); err != nil {
			t.Fatalf("sync %d: %v", i+1, err)
		}
	}

	if torrent, ok := sm.GetTorrentUnchecked("def"); !ok || torrent.Name != "second" {
		t.Fatalf("expected the new torrent in the cache, got %+v (found %v)", torrent, ok)
	}
	if called {
		t.Fatal("expected no callback for a complete new torrent")
	}
	if want, got := []string{"0", "5", "6"}, script.seenRids(); !slices.Equal(got, want) {
		t.Fatalf("expected rids %v, got %v", want, got)
	}
}

func TestSyncManager_DiffForKnownTorrentWithoutAddedOn(t *testing.T) {
	script := &syncScript{replies: []func(http.ResponseWriter, *http.Request){
		sendBody(fullUpdateOneTorrent),
		sendBody(`{"rid":6,"torrents":{"abc":{"progress":1}}}`),
		sendBody(`{"rid":7}`),
	}}
	server := newScriptedSyncServer(t, "", script)

	called := false
	options := DefaultSyncOptions()
	options.OnTorrentsLeftOut = func([]string) { called = true }
	sm := NewSyncManager(NewClient(Config{Host: server.URL, APIKey: "test-key", RetryAttempts: 1}), options)

	for i := range 3 {
		if err := sm.Sync(t.Context()); err != nil {
			t.Fatalf("sync %d: %v", i+1, err)
		}
	}

	torrent, _ := sm.GetTorrentUnchecked("abc")
	if torrent.Progress != 1 || torrent.Name != "first" || torrent.AddedOn != 1700000000 {
		t.Fatalf("expected the diff merged into the known torrent, got %+v", torrent)
	}
	if called {
		t.Fatal("expected no callback for a known torrent")
	}
	if want, got := []string{"0", "5", "6"}, script.seenRids(); !slices.Equal(got, want) {
		t.Fatalf("expected rids %v, got %v", want, got)
	}
}

// holdUntilClientLeaves never answers, so the attempt ends at the client timeout.
func holdUntilClientLeaves(_ http.ResponseWriter, r *http.Request) {
	<-r.Context().Done()
}

// closeConnection reads the request and closes the connection with no answer.
func closeConnection(w http.ResponseWriter, _ *http.Request) {
	conn, _, err := http.NewResponseController(w).Hijack()
	if err == nil {
		_ = conn.Close()
	}
}

// sendBodyOnNewConnection makes the client open a new connection for the next
// request. The transport sends a GET again by itself when a reused connection
// closes, and the test would not see that.
func sendBodyOnNewConnection(body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Connection", "close")
		_, _ = io.WriteString(w, body)
	}
}

// qBittorrent 5.2.4 after it lost the response that added def: the buffer for
// rid 5 holds only the last changed fields, and a full update holds all.
const (
	lostDiffReply  = `{"rid":7,"torrents":{"def":{"progress":1,"state":"stalledUP"}}}`
	fixedDiffReply = `{"rid":7,"torrents":{"def":{"added_on":1700000500,"name":"second","size":4096,"progress":1,"state":"stalledUP"}}}`
)

func replyByRid(diff string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("rid") == "0" {
			_, _ = io.WriteString(w, fullUpdateTwoTorrents)
			return
		}
		_, _ = io.WriteString(w, diff)
	}
}

func newLostAttemptClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	client := NewClient(Config{Host: server.URL, APIKey: "test-key"}).
		WithHTTPClient(&http.Client{Timeout: 200 * time.Millisecond})
	client.retryDelay = time.Millisecond
	return client
}

func TestSyncManager_RetryAfterLostAttempt(t *testing.T) {
	tests := []struct {
		name          string
		webAPIVersion string
		lost          func(http.ResponseWriter, *http.Request)
		diff          string
		wantRids      []string
	}{
		{name: "5.2 timeout", webAPIVersion: "2.15.1", lost: holdUntilClientLeaves, diff: lostDiffReply, wantRids: []string{"0", "5", "0"}},
		{name: "5.2 closed connection", webAPIVersion: "2.15.1", lost: closeConnection, diff: lostDiffReply, wantRids: []string{"0", "5", "0"}},
		{name: "unknown version timeout", lost: holdUntilClientLeaves, diff: lostDiffReply, wantRids: []string{"0", "5", "0"}},
		{name: "5.3 timeout", webAPIVersion: "2.16.2", lost: holdUntilClientLeaves, diff: fixedDiffReply, wantRids: []string{"0", "5", "5"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script := &syncScript{replies: []func(http.ResponseWriter, *http.Request){
				sendBodyOnNewConnection(fullUpdateOneTorrent),
				tt.lost,
				replyByRid(tt.diff),
			}}
			server := newScriptedSyncServer(t, tt.webAPIVersion, script)
			client := newLostAttemptClient(t, server)
			if tt.webAPIVersion != "" {
				if _, err := client.GetWebAPIVersionCtx(t.Context()); err != nil {
					t.Fatalf("read the version: %v", err)
				}
			}

			var errs []error
			leftOut := false
			options := DefaultSyncOptions()
			options.OnError = func(err error) { errs = append(errs, err) }
			options.OnTorrentsLeftOut = func([]string) { leftOut = true }
			sm := NewSyncManager(client, options)

			for i := range 2 {
				if err := sm.Sync(t.Context()); err != nil {
					t.Fatalf("sync %d: %v", i+1, err)
				}
			}

			if got := script.seenRids(); !slices.Equal(got, tt.wantRids) {
				t.Fatalf("expected rids %v, got %v", tt.wantRids, got)
			}
			torrent, ok := sm.GetTorrentUnchecked("def")
			if !ok || torrent.Name != "second" || torrent.AddedOn != 1700000500 || torrent.Size != 4096 || torrent.Progress != 1 {
				t.Fatalf("expected the complete torrent, got %+v (found %v)", torrent, ok)
			}
			if len(errs) > 0 || leftOut {
				t.Fatalf("expected a silent recovery, got errors %v, left out %v", errs, leftOut)
			}
		})
	}
}

// The full update that replaces a retry gets one attempt. When it is lost
// too, the sync fails, and the next sync asks for a full update.
func TestSyncManager_LostFullUpdateAfterLostAttemptFailsTheSync(t *testing.T) {
	script := &syncScript{replies: []func(http.ResponseWriter, *http.Request){
		sendBodyOnNewConnection(fullUpdateOneTorrent),
		holdUntilClientLeaves,
		holdUntilClientLeaves,
		sendBody(fullUpdateTwoTorrents),
	}}
	server := newScriptedSyncServer(t, "", script)
	sm := NewSyncManager(newLostAttemptClient(t, server))

	if err := sm.Sync(t.Context()); err != nil {
		t.Fatalf("sync 1: %v", err)
	}
	if err := sm.Sync(t.Context()); err == nil {
		t.Fatal("expected the sync to fail after the lost full update")
	}
	if err := sm.Sync(t.Context()); err != nil {
		t.Fatalf("sync 3: %v", err)
	}
	if want, got := []string{"0", "5", "0", "0"}, script.seenRids(); !slices.Equal(got, want) {
		t.Fatalf("expected rids %v, got %v", want, got)
	}
}

// A 403 comes before qBittorrent builds a sync response, so the attempt after
// the new login keeps its rid.
func TestSyncManager_RetryAfterLoginKeepsRid(t *testing.T) {
	script := &syncScript{replies: []func(http.ResponseWriter, *http.Request){
		sendBody(fullUpdateOneTorrent),
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) },
		sendBody(`{"rid":6,"torrents":{"abc":{"progress":1}}}`),
	}}
	mux := http.NewServeMux()
	mux.Handle("GET /api/v2/sync/maindata", script)
	mux.HandleFunc("POST /api/v2/auth/login", func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: "session"})
		_, _ = io.WriteString(w, "Ok.")
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	client := NewClient(Config{Host: server.URL, Username: "user", Password: "pass"})
	client.retryDelay = time.Millisecond
	sm := NewSyncManager(client)

	for i := range 2 {
		if err := sm.Sync(t.Context()); err != nil {
			t.Fatalf("sync %d: %v", i+1, err)
		}
	}
	if want, got := []string{"0", "5", "5"}, script.seenRids(); !slices.Equal(got, want) {
		t.Fatalf("expected rids %v, got %v", want, got)
	}
}
