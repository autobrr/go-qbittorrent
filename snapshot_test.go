package qbittorrent

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

// newSnapshotSyncManager returns a manager whose sync/maindata action plays
// script.
func newSnapshotSyncManager(t *testing.T, script *syncScript, options SyncOptions) *SyncManager {
	t.Helper()
	server := newScriptedSyncServer(t, "", script)
	return NewSyncManager(NewClient(Config{Host: server.URL, APIKey: "test-key", RetryAttempts: 1}), options)
}

func syncOnce(t *testing.T, sm *SyncManager) {
	t.Helper()
	require.NoError(t, sm.Sync(t.Context()))
}

// libraryFullUpdate returns a full update with n torrents named t0 to t<n-1>.
func libraryFullUpdate(rid, n int) string {
	torrents := make([]string, n)
	for i := range n {
		torrents[i] = fmt.Sprintf(`"t%d":{"added_on":%d,"name":"torrent %d","size":%d,"state":"stalledUP","progress":1}`, i, 1700000000+i, i, 1000+i)
	}
	return fmt.Sprintf(`{"rid":%d,"full_update":true,"torrents":{%s},"categories":{},"tags":[],"server_state":{}}`, rid, strings.Join(torrents, ","))
}

func snapshotHashes(s *SyncSnapshot) []string {
	var hashes []string
	for _, t := range s.Torrents() {
		hashes = append(hashes, t.Hash)
	}
	return hashes
}

func TestSyncSnapshot_ReadersOfOneSyncShareTorrents(t *testing.T) {
	sm := newSnapshotSyncManager(t, &syncScript{replies: []func(http.ResponseWriter, *http.Request){
		sendBody(fullUpdateTwoTorrents),
	}}, DefaultSyncOptions())
	syncOnce(t, sm)

	first, second := sm.Snapshot(), sm.SnapshotUnchecked()
	require.NotNil(t, first)
	require.Equal(t, 2, first.Len())
	require.Equal(t, first.Torrents(), second.Torrents())
	for _, hash := range []string{"abc", "def"} {
		a, ok := first.Torrent(hash)
		require.True(t, ok)
		b, ok := second.Torrent(hash)
		require.True(t, ok)
		require.Same(t, a, b, hash)
	}
	matched := second.Matching(func(t *Torrent) bool { return t.Name == "second" })
	require.Len(t, matched, 1)
	def, _ := first.Torrent("def")
	require.Same(t, def, matched[0])
}

func TestSyncSnapshot_PartialSyncMakesObjectsOnlyForChangedTorrents(t *testing.T) {
	sm := newSnapshotSyncManager(t, &syncScript{replies: []func(http.ResponseWriter, *http.Request){
		sendBody(libraryFullUpdate(1, 1000)),
		sendBody(`{"rid":2,"torrents":{"t7":{"progress":0.5},"t500":{"name":"renamed"},"t999":{"dlspeed":42}}}`),
	}}, DefaultSyncOptions())
	syncOnce(t, sm)
	before := sm.SnapshotUnchecked()
	syncOnce(t, sm)
	after := sm.SnapshotUnchecked()

	require.Equal(t, before.Number()+1, after.Number())
	require.Equal(t, 1000, after.Len())
	var same, changed []string
	for _, old := range before.Torrents() {
		now, ok := after.Torrent(old.Hash)
		require.True(t, ok)
		if now == old {
			same = append(same, old.Hash)
		} else {
			changed = append(changed, old.Hash)
		}
	}
	require.Len(t, same, 997)
	require.ElementsMatch(t, []string{"t7", "t500", "t999"}, changed)

	t7, _ := after.Torrent("t7")
	require.Equal(t, 0.5, t7.Progress)
	require.Equal(t, "torrent 7", t7.Name)
	t500, _ := after.Torrent("t500")
	require.Equal(t, "renamed", t500.Name)
}

func TestSyncSnapshot_HeldSnapshotDoesNotChange(t *testing.T) {
	sm := newSnapshotSyncManager(t, &syncScript{replies: []func(http.ResponseWriter, *http.Request){
		sendBody(fullUpdateTwoTorrents),
		sendBody(`{"rid":2,"torrents":{"abc":{"progress":0.25,"name":"changed"}}}`),
		sendBody(`{"rid":3,"torrents":{"ghi":{"added_on":1700000900,"name":"third"}}}`),
		sendBody(`{"rid":4,"torrents_removed":["def"]}`),
		sendBody(`{"rid":5,"full_update":true,"torrents":{"xyz":{"added_on":1700001000,"name":"only"}},"categories":{},"tags":[],"server_state":{}}`),
	}}, DefaultSyncOptions())
	syncOnce(t, sm)
	held := sm.SnapshotUnchecked()
	number, at, refs := held.Number(), held.Time(), held.Torrents()
	values := copyTorrents(refs)

	for range 4 {
		syncOnce(t, sm)
	}

	require.Equal(t, []string{"xyz"}, snapshotHashes(sm.SnapshotUnchecked()))
	require.Equal(t, number, held.Number())
	require.Equal(t, at, held.Time())
	require.Equal(t, refs, held.Torrents())
	require.Equal(t, values, copyTorrents(held.Torrents()))
	_, ok := held.Torrent("ghi")
	require.False(t, ok)
}

func TestSyncSnapshot_FailedSyncPublishesNothing(t *testing.T) {
	for name, reply := range map[string]func(http.ResponseWriter, *http.Request){
		"status":   func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
		"bad body": sendBody(`{"rid":2,"torrents":{"abc":{"progress":`),
	} {
		t.Run(name, func(t *testing.T) {
			var updates []SyncUpdate
			options := DefaultSyncOptions()
			options.OnSyncUpdate = func(u SyncUpdate) { updates = append(updates, u) }
			sm := newSnapshotSyncManager(t, &syncScript{replies: []func(http.ResponseWriter, *http.Request){
				sendBody(fullUpdateTwoTorrents),
				reply,
			}}, options)
			syncOnce(t, sm)
			last := sm.SnapshotUnchecked()

			require.Error(t, sm.Sync(t.Context()))

			require.Same(t, last, sm.SnapshotUnchecked())
			require.Same(t, last, sm.Snapshot())
			abc, _ := last.Torrent("abc")
			require.Equal(t, float64(1), abc.Progress)
			require.Len(t, updates, 1, "a failed sync must not call OnSyncUpdate")
		})
	}
}

func TestSyncSnapshot_NoSnapshotBeforeFirstSync(t *testing.T) {
	options := DefaultSyncOptions()
	options.DynamicSync = false
	sm := newSnapshotSyncManager(t, &syncScript{replies: []func(http.ResponseWriter, *http.Request){
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
	}}, options)
	require.Nil(t, sm.SnapshotUnchecked())
	require.Nil(t, sm.Snapshot())
	require.Error(t, sm.Sync(t.Context()))
	require.Nil(t, sm.SnapshotUnchecked())
}

func TestSyncSnapshot_FullUpdateKeepsEqualTorrents(t *testing.T) {
	sm := newSnapshotSyncManager(t, &syncScript{replies: []func(http.ResponseWriter, *http.Request){
		sendBody(libraryFullUpdate(1, 50)),
		sendBody(strings.Replace(libraryFullUpdate(9, 50), `"name":"torrent 3"`, `"name":"renamed"`, 1)),
	}}, DefaultSyncOptions())
	syncOnce(t, sm)
	before := sm.SnapshotUnchecked()
	syncOnce(t, sm)
	after := sm.SnapshotUnchecked()

	var changed []string
	for _, old := range before.Torrents() {
		now, ok := after.Torrent(old.Hash)
		require.True(t, ok)
		if now != old {
			changed = append(changed, old.Hash)
		}
	}
	require.Equal(t, []string{"t3"}, changed)
	t3, _ := after.Torrent("t3")
	require.Equal(t, "renamed", t3.Name)
}

func TestSyncSnapshot_OrderIsStable(t *testing.T) {
	sm := newSnapshotSyncManager(t, &syncScript{replies: []func(http.ResponseWriter, *http.Request){
		sendBody(libraryFullUpdate(1, 30)),
		sendBody(`{"rid":2,"torrents":{"t4":{"progress":0.5},"new1":{"added_on":1800000000,"name":"new"}},"torrents_removed":["t9"]}`),
		sendBody(`{"rid":3,"torrents":{"new0":{"added_on":1800000001,"name":"newer"},"t20":{"dlspeed":5}}}`),
		sendBody(strings.Replace(libraryFullUpdate(4, 31), `"t30":`, `"later":`, 1)),
	}}, DefaultSyncOptions())

	var orders [][]string
	for range 4 {
		syncOnce(t, sm)
		orders = append(orders, snapshotHashes(sm.SnapshotUnchecked()))
	}
	for i := 1; i < len(orders); i++ {
		var kept []string
		for _, hash := range orders[i-1] {
			if slices.Contains(orders[i], hash) {
				kept = append(kept, hash)
			}
		}
		require.Equal(t, kept, orders[i][:len(kept)], "sync %d changed the order of the kept torrents", i+1)
		for _, hash := range orders[i][len(kept):] {
			require.NotContains(t, orders[i-1], hash, "sync %d put a kept torrent after a new one", i+1)
		}
	}
	require.Equal(t, "new1", orders[1][len(orders[1])-1])
	require.Equal(t, "new0", orders[2][len(orders[2])-1])
	require.ElementsMatch(t, []string{"later", "t9"}, orders[3][len(orders[3])-2:])
}

func TestSyncSnapshot_SealPanicsWithHashOfChangedTorrent(t *testing.T) {
	for _, seal := range []bool{false, true} {
		t.Run(fmt.Sprintf("seal=%v", seal), func(t *testing.T) {
			options := DefaultSyncOptions()
			options.SealSnapshots = seal
			sm := newSnapshotSyncManager(t, &syncScript{replies: []func(http.ResponseWriter, *http.Request){
				sendBody(fullUpdateTwoTorrents),
				sendBody(`{"rid":2,"server_state":{"dl_info_speed":1}}`),
			}}, options)
			syncOnce(t, sm)
			syncOnce(t, sm)
			def, _ := sm.SnapshotUnchecked().Torrent("def")
			def.Name = "changed by a reader"

			var recovered any
			func() {
				defer func() { recovered = recover() }()
				_ = sm.Sync(t.Context())
			}()
			if !seal {
				require.Nil(t, recovered)
				return
			}
			require.NotNil(t, recovered, "the sync after a write to a sealed torrent must panic")
			require.Contains(t, fmt.Sprint(recovered), "torrent def ")
		})
	}
}

func TestSyncManager_OnSyncUpdate(t *testing.T) {
	var mu sync.Mutex
	var updates []SyncUpdate
	options := DefaultSyncOptions()
	options.OnSyncUpdate = func(u SyncUpdate) {
		mu.Lock()
		defer mu.Unlock()
		updates = append(updates, u)
	}
	sm := newSnapshotSyncManager(t, &syncScript{replies: []func(http.ResponseWriter, *http.Request){
		sendBody(`{"rid":1,"full_update":true,"torrents":{"abc":{"added_on":1700000000,"name":"first"}},"categories":{"tv":{"name":"tv","savePath":"/tv"}},"tags":["x"],"trackers":{"http://tracker.test/a":["abc"]},"server_state":{"dl_info_speed":3}}`),
		sendBody(`{"rid":2,"torrents":{"abc":{"progress":0.5}}}`),
		sendBody(`{"rid":3,"torrents":{"def":{"added_on":1700000500,"name":"second"}}}`),
		sendBody(`{"rid":4,"torrents_removed":["abc"]}`),
		sendBody(`{"rid":5}`),
		// Last, because the sync after a torrent that is left out asks for rid 0.
		sendBody(`{"rid":6,"torrents":{"ghi":{"added_on":1700000900,"name":"third"},"zzz":{"progress":1}}}`),
	}}, options)

	snapshots := []*SyncSnapshot{}
	for range 6 {
		syncOnce(t, sm)
		snapshots = append(snapshots, sm.SnapshotUnchecked())
	}

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, updates, 5, "a sync that changed nothing calls no callback")
	changed := append(snapshots[:4:4], snapshots[5])
	for i, u := range updates {
		require.Same(t, changed[i], u.Snapshot, "update %d", i)
	}

	full := updates[0]
	require.Equal(t, []string{"abc"}, full.Changes.Torrents)
	require.True(t, full.Changes.Categories && full.Changes.Tags && full.Changes.Trackers && full.Changes.ServerState)
	require.Equal(t, "/tv", full.Categories["tv"].SavePath)
	require.Equal(t, []string{"x"}, full.Tags)
	require.Equal(t, []string{"abc"}, full.Trackers["http://tracker.test/a"])
	require.Equal(t, int64(3), full.ServerState.DlInfoSpeed)

	change := updates[1]
	require.Equal(t, []string{"abc"}, change.Changes.Torrents)
	require.Empty(t, change.Changes.TorrentsRemoved)
	require.Empty(t, change.Changes.TorrentsLeftOut)
	require.Equal(t, int64(3), change.ServerState.DlInfoSpeed)

	add := updates[2]
	require.Equal(t, []string{"def"}, add.Changes.Torrents)
	require.Empty(t, add.Changes.TorrentsLeftOut)

	del := updates[3]
	require.Empty(t, del.Changes.Torrents)
	require.Equal(t, []string{"abc"}, del.Changes.TorrentsRemoved)
	require.Equal(t, []string{"def"}, snapshotHashes(del.Snapshot))
	require.Empty(t, del.Trackers, "the removed torrent leaves the tracker map")

	leftOut := updates[4]
	require.Equal(t, []string{"ghi"}, leftOut.Changes.Torrents)
	require.Equal(t, []string{"zzz"}, leftOut.Changes.TorrentsLeftOut)
	_, ok := leftOut.Snapshot.Torrent("zzz")
	require.False(t, ok)
}

func TestSyncManager_OnUpdateGetsAllData(t *testing.T) {
	var updates []*MainData
	options := DefaultSyncOptions()
	options.OnUpdate = func(data *MainData) { updates = append(updates, data) }
	sm := newSnapshotSyncManager(t, &syncScript{replies: []func(http.ResponseWriter, *http.Request){
		sendBody(fullUpdateTwoTorrents),
		sendBody(`{"rid":2,"torrents":{"abc":{"progress":0.5}},"torrents_removed":["def"],"server_state":{"dl_info_speed":9}}`),
	}}, options)
	syncOnce(t, sm)
	syncOnce(t, sm)

	require.Len(t, updates, 2)
	require.Len(t, updates[0].Torrents, 2)
	require.Equal(t, int64(2), updates[1].Rid)
	require.Equal(t, map[string]Torrent{"abc": {Hash: "abc", AddedOn: 1700000000, Name: "first", Progress: 0.5}}, updates[1].Torrents)
	require.Equal(t, []string{"abc"}, updates[1].Changes.Torrents)
	require.Equal(t, []string{"def"}, updates[1].Changes.TorrentsRemoved)
	require.Equal(t, int64(9), updates[1].ServerState.DlInfoSpeed)
	require.Equal(t, sm.GetDataUnchecked(), updates[1])

	// The callback owns its copy.
	updates[1].Torrents["abc"] = Torrent{Name: "changed"}
	abc, _ := sm.GetTorrentUnchecked("abc")
	require.Equal(t, "first", abc.Name)
}

func TestSyncManager_GetTorrentsLimitDoesNotOverflow(t *testing.T) {
	sm, hashes := newLibrarySyncManager(3)

	torrents := sm.GetTorrentsUnchecked(TorrentFilterOptions{Sort: "hash", Offset: 1, Limit: math.MaxInt})

	got := make([]string, len(torrents))
	for i, torrent := range torrents {
		got[i] = torrent.Hash
	}
	require.Equal(t, hashes[1:], got)
}

// syncLibrary serves a full update of n torrents with all fields, and then
// diffs that each change the speeds of the first changes torrents.
type syncLibrary struct {
	full  []byte
	diffs [2][]byte
	next  atomic.Int64
}

func newSyncLibrary(n, changes int) *syncLibrary {
	torrents := make(map[string]Torrent, n)
	for i := range n {
		hash := fmt.Sprintf("%040x", i)
		torrents[hash] = Torrent{
			AddedOn:      int64(1700000000 + i),
			Category:     "movies",
			ContentPath:  fmt.Sprintf("/data/movies/Some.Movie.%d.2160p.WEB-DL", i),
			Hash:         hash,
			HasMetadata:  new(true),
			InfohashV1:   hash,
			MagnetURI:    fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=Some.Movie.%d", hash, i),
			Name:         fmt.Sprintf("Some.Movie.%d.2160p.WEB-DL", i),
			Progress:     1,
			SavePath:     "/data/movies",
			Size:         int64(i) * 1 << 20,
			State:        TorrentStateStalledUp,
			Tags:         "a,b",
			Tracker:      fmt.Sprintf("https://tracker%d.test/announce", i%40),
			TotalSize:    int64(i) * 1 << 20,
			TimeActive:   int64(i),
			Ratio:        1.5,
			SeedingTime:  int64(i),
			LastActivity: int64(1700000000 + i),
		}
	}
	full, err := json.Marshal(map[string]any{"rid": 1, "full_update": true, "torrents": torrents, "categories": map[string]any{}, "tags": []string{}, "server_state": map[string]any{}})
	if err != nil {
		panic(err)
	}
	lib := &syncLibrary{full: full}
	for d := range lib.diffs {
		diff := make(map[string]any, changes)
		for i := range changes {
			diff[fmt.Sprintf("%040x", i)] = map[string]any{"dlspeed": d + 1, "upspeed": d + 2, "last_activity": 1800000000 + d}
		}
		lib.diffs[d], err = json.Marshal(map[string]any{"rid": 2 + d, "torrents": diff})
		if err != nil {
			panic(err)
		}
	}
	return lib
}

func (l *syncLibrary) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body := l.full
	if r.URL.Query().Get("rid") != "0" {
		body = l.diffs[(l.next.Add(1)-1)%2]
	}
	_, _ = w.Write(body)
}

func newLibraryManager(tb testing.TB, lib *syncLibrary, options SyncOptions) *SyncManager {
	tb.Helper()
	server := httptest.NewServer(lib)
	tb.Cleanup(server.Close)
	sm := NewSyncManager(NewClient(Config{Host: server.URL, APIKey: "test-key", RetryAttempts: 1}), options)
	if err := sm.Sync(tb.Context()); err != nil {
		tb.Fatal(err)
	}
	return sm
}

// bytesPerSync returns the fewest bytes that one sync of sm allocated over
// a few syncs.
func bytesPerSync(t *testing.T, sm *SyncManager) uint64 {
	t.Helper()
	var least uint64
	for i := range 5 {
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		syncOnce(t, sm)
		runtime.ReadMemStats(&after)
		if n := after.TotalAlloc - before.TotalAlloc; i == 0 || n < least {
			least = n
		}
	}
	return least
}

func TestSyncManager_OnSyncUpdateCopiesNoTorrents(t *testing.T) {
	const torrents = 20_000
	lib := newSyncLibrary(torrents, 100)
	copyOfAll := uint64(torrents * unsafe.Sizeof(Torrent{}))

	oldOptions := DefaultSyncOptions()
	oldOptions.OnUpdate = func(*MainData) {}
	withCopy := bytesPerSync(t, newLibraryManager(t, lib, oldOptions))

	newOptions := DefaultSyncOptions()
	newOptions.OnSyncUpdate = func(SyncUpdate) {}
	withoutCopy := bytesPerSync(t, newLibraryManager(t, lib, newOptions))

	t.Logf("bytes per sync: OnUpdate %d, OnSyncUpdate %d", withCopy, withoutCopy)
	require.Greater(t, withCopy, copyOfAll, "OnUpdate gets a copy of all torrents")
	require.Less(t, withoutCopy, copyOfAll/10, "OnSyncUpdate must get no copy of the torrents")
}
