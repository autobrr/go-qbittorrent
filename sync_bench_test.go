package qbittorrent

import (
	"fmt"
	"testing"
	"time"
)

func createBenchSyncManager() *SyncManager {
	sm := &SyncManager{
		client:  newOfflineClient(),
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
	sm.lastSync = time.Now()
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
	sm.data = &MainData{Torrents: torrents}
	sm.updateAllTorrents()
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

// BenchmarkSyncManager_UpdateAllTorrents measures the cache rebuild that runs
// after each sync.
func BenchmarkSyncManager_UpdateAllTorrents(b *testing.B) {
	sm, _ := newLibrarySyncManager(50_000)
	for b.Loop() {
		sm.updateAllTorrents()
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
		sm.applySyncResult(source, raw, time.Now(), nil)
	}
}
