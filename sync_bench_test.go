package qbittorrent

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"
)

func createBenchSyncManager() *SyncManager {
	mockClient := NewMockClient()
	sm := &SyncManager{
		client:  mockClient.Client,
		options: DefaultSyncOptions(),
		data: &MainData{
			Rid:        1,
			FullUpdate: true,
			Torrents: map[string]Torrent{
				"hash1": {Hash: "hash1", Name: "Test Torrent 1", State: "downloading"},
				"hash2": {Hash: "hash2", Name: "Test Torrent 2", State: "seeding"},
				"hash3": {Hash: "hash3", Name: "Test Torrent 3", State: "paused"},
			},
			ServerState: ServerState{
				DlInfoSpeed: 1024000,
				UpInfoSpeed: 512000,
			},
			Categories: make(map[string]Category),
			Tags:       []string{},
			Trackers:   make(map[string][]string),
		},
	}
	seedSyncManager(sm, sm.data)
	setLastSync(sm, time.Now())
	sm.options.DynamicSync = true
	sm.options.MinSyncInterval = 1 * time.Second

	return sm
}

// Benchmark for GetTorrents with freshness checking
func BenchmarkSyncManager_GetTorrents(b *testing.B) {
	syncManager := createBenchSyncManager()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = syncManager.GetTorrents(TorrentFilterOptions{})
		}
	})
}

// Benchmark for GetTorrentsUnchecked without freshness checking
func BenchmarkSyncManager_GetTorrentsUnchecked(b *testing.B) {
	syncManager := createBenchSyncManager()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = syncManager.GetTorrentsUnchecked(TorrentFilterOptions{})
		}
	})
}

// Benchmark for GetTorrent with freshness checking
func BenchmarkSyncManager_GetTorrent(b *testing.B) {
	syncManager := createBenchSyncManager()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = syncManager.GetTorrent("hash1")
		}
	})
}

// Benchmark for GetTorrentUnchecked without freshness checking
func BenchmarkSyncManager_GetTorrentUnchecked(b *testing.B) {
	syncManager := createBenchSyncManager()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = syncManager.GetTorrentUnchecked("hash1")
		}
	})
}

// Benchmark multiple sequential gets with the old approach (simulated)
func BenchmarkSyncManager_MultipleSequentialGets(b *testing.B) {
	syncManager := createBenchSyncManager()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate getting multiple pieces of data in quick succession
		_ = syncManager.GetTorrents(TorrentFilterOptions{})
		_, _ = syncManager.GetTorrent("hash1")
		_ = syncManager.GetServerState()
		_ = syncManager.GetCategories()
	}
}

// Benchmark multiple sequential gets with unchecked methods
func BenchmarkSyncManager_MultipleSequentialGetsUnchecked(b *testing.B) {
	syncManager := createBenchSyncManager()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate getting multiple pieces of data in quick succession
		_ = syncManager.GetTorrentsUnchecked(TorrentFilterOptions{})
		_, _ = syncManager.GetTorrentUnchecked("hash1")
		_ = syncManager.GetServerStateUnchecked()
		_ = syncManager.GetCategoriesUnchecked()
	}
}

// newLibrarySyncManager returns a manager that holds n torrents with mixed
// categories, tags, states and sizes. Sizes repeat, so a sort by size has ties.
func newLibrarySyncManager(n int) (*SyncManager, []string) {
	categories := []string{"", "movies", "tv"}
	tags := []string{"", "a", "a,b", "b"}
	states := []TorrentState{TorrentStateUploading, TorrentStatePausedUp, TorrentStateDownloading, TorrentStateStalledDl, TorrentStateError}

	torrents := make(map[string]Torrent, n)
	hashes := make([]string, 0, n)
	for i := range n {
		hash := fmt.Sprintf("%040x", i)
		hashes = append(hashes, hash)
		torrents[hash] = Torrent{
			Hash:     hash,
			Name:     fmt.Sprintf("torrent %d", i),
			Category: categories[i%len(categories)],
			Tags:     tags[i%len(tags)],
			State:    states[i%len(states)],
			Size:     int64(i % 7),
			AddedOn:  int64(n - i),
		}
	}

	sm := NewSyncManager(nil)
	seedSyncManager(sm, &MainData{Torrents: torrents})
	return sm, hashes
}

func BenchmarkSyncManager_GetTorrentsByHash(b *testing.B) {
	sm, hashes := newLibrarySyncManager(50_000)

	for _, count := range []int{1, 1_000, 50_000} {
		b.Run(fmt.Sprintf("hashes=%d", count), func(b *testing.B) {
			options := TorrentFilterOptions{Hashes: hashes[:count]}
			for b.Loop() {
				_ = sm.GetTorrentsUnchecked(options)
			}
		})
	}
}

// BenchmarkSyncManager_Sync measures one sync of a library of 20,000
// torrents in which 100 torrents change, with each kind of update callback.
func BenchmarkSyncManager_Sync(b *testing.B) {
	lib := newSyncLibrary(20_000, 100)
	for _, tc := range []struct {
		name string
		set  func(*SyncOptions)
	}{
		{"callback=none", func(*SyncOptions) {}},
		{"callback=OnUpdate", func(o *SyncOptions) { o.OnUpdate = func(*MainData) {} }},
		{"callback=OnSyncUpdate", func(o *SyncOptions) { o.OnSyncUpdate = func(SyncUpdate) {} }},
	} {
		b.Run(tc.name, func(b *testing.B) {
			options := DefaultSyncOptions()
			tc.set(&options)
			sm := newLibraryManager(b, lib, options)
			b.ReportAllocs()
			for b.Loop() {
				if err := sm.Sync(b.Context()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkClient_SyncMainData(b *testing.B) {
	lib := newSyncLibrary(20_000, 5_000)
	for _, tc := range []struct {
		name string
		rid  int64
		body []byte
	}{
		{"response=full/rid=0", 0, lib.full},
		{"response=full/rid=7", 7, lib.full},
		{"response=update/rid=7", 7, lib.diffs[0]},
	} {
		b.Run(tc.name, func(b *testing.B) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(tc.body)
			}))
			b.Cleanup(server.Close)
			client := NewClient(Config{Host: server.URL, APIKey: "test-key", RetryAttempts: 1})
			b.ReportAllocs()
			for b.Loop() {
				if _, _, err := client.SyncMainDataCtxWithRaw(b.Context(), tc.rid); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkSyncManager_ApplyUnchangedSync measures a sync response that
// changes nothing, while an OnUpdate callback is set.
func BenchmarkSyncManager_ApplyUnchangedSync(b *testing.B) {
	sm, _ := newLibrarySyncManager(50_000)
	sm.options.OnUpdate = func(*MainData) {}
	source := &MainData{Rid: 2}
	raw := map[string]any{"rid": float64(2)}
	for b.Loop() {
		next, changes := buildSnapshot(sm.SnapshotUnchecked(), source, raw)
		sm.applySyncResult(source, raw, next, changes, time.Now(), nil)
	}
}

// BenchmarkSyncManager_MemoryAtRest reports the heap that a manager keeps
// for a library of 20,000 torrents after a full sync and one diff.
func BenchmarkSyncManager_MemoryAtRest(b *testing.B) {
	lib := newSyncLibrary(20_000, 100)
	var held uint64
	for b.Loop() {
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		sm := newLibraryManager(b, lib, DefaultSyncOptions())
		if err := sm.Sync(b.Context()); err != nil {
			b.Fatal(err)
		}
		runtime.GC()
		runtime.ReadMemStats(&after)
		held = after.HeapAlloc - before.HeapAlloc
		runtime.KeepAlive(sm)
	}
	b.ReportMetric(float64(held), "heap-B")
}

func BenchmarkSyncManager_GetTorrentsMatching(b *testing.B) {
	sm, _ := newLibrarySyncManager(50_000)
	for _, tc := range []struct {
		name    string
		options TorrentFilterOptions
		match   func(*Torrent) bool
	}{
		{"category", TorrentFilterOptions{Category: "movies"}, func(t *Torrent) bool { return t.Category == "movies" }},
		{"state", TorrentFilterOptions{Filter: TorrentFilterError}, func(t *Torrent) bool { return t.State == TorrentStateError }},
		{"none", TorrentFilterOptions{Category: "none"}, func(t *Torrent) bool { return false }},
	} {
		b.Run(tc.name+"/matching", func(b *testing.B) {
			for b.Loop() {
				_ = sm.GetTorrentsMatchingUnchecked(tc.match)
			}
		})
		b.Run(tc.name+"/options", func(b *testing.B) {
			for b.Loop() {
				_ = sm.GetTorrentsUnchecked(tc.options)
			}
		})
	}
}
