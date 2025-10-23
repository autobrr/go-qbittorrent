package qbittorrent

import (
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
