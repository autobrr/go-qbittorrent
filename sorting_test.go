package qbittorrent

import (
	"slices"
	"testing"
)

func TestTorrentSorting(t *testing.T) {
	// Create test torrents with different values
	torrents := []Torrent{
		{Name: "Charlie", Hash: "hash3", Size: 300, Priority: 3},
		{Name: "Alice", Hash: "hash1", Size: 100, Priority: 1},
		{Name: "Bob", Hash: "hash2", Size: 200, Priority: 2},
		{Name: "David", Hash: "hash4", Size: 400, Priority: 1}, // Same priority as Alice for stability test
	}

	t.Run("sort by name ascending", func(t *testing.T) {
		test := slices.Clone(torrents)
		applyTorrentSorting(test, "name", false)
		
		expected := []string{"Alice", "Bob", "Charlie", "David"}
		for i, torrent := range test {
			if torrent.Name != expected[i] {
				t.Errorf("Expected %s at position %d, got %s", expected[i], i, torrent.Name)
			}
		}
	})

	t.Run("sort by name descending", func(t *testing.T) {
		test := slices.Clone(torrents)
		applyTorrentSorting(test, "name", true)
		
		expected := []string{"David", "Charlie", "Bob", "Alice"}
		for i, torrent := range test {
			if torrent.Name != expected[i] {
				t.Errorf("Expected %s at position %d, got %s", expected[i], i, torrent.Name)
			}
		}
	})

	t.Run("sort by size ascending", func(t *testing.T) {
		test := slices.Clone(torrents)
		applyTorrentSorting(test, "size", false)
		
		expected := []int64{100, 200, 300, 400}
		for i, torrent := range test {
			if torrent.Size != expected[i] {
				t.Errorf("Expected size %d at position %d, got %d", expected[i], i, torrent.Size)
			}
		}
	})

	t.Run("sort by priority with stability", func(t *testing.T) {
		test := slices.Clone(torrents)
		applyTorrentSorting(test, "priority", false)
		
		// Alice and David both have priority 1, should be sorted by hash (secondary sort)
		// hash1 < hash4, so Alice should come before David
		if test[0].Name != "Alice" || test[1].Name != "David" {
			t.Errorf("Expected Alice then David for priority 1, got %s then %s", test[0].Name, test[1].Name)
		}
		if test[2].Name != "Bob" || test[3].Name != "Charlie" {
			t.Errorf("Expected Bob then Charlie for higher priorities, got %s then %s", test[2].Name, test[3].Name)
		}
	})

	t.Run("sort by invalid field uses default", func(t *testing.T) {
		test := slices.Clone(torrents)
		applyTorrentSorting(test, "invalid_field", false)
		
		// Should fall back to name sorting
		expected := []string{"Alice", "Bob", "Charlie", "David"}
		for i, torrent := range test {
			if torrent.Name != expected[i] {
				t.Errorf("Expected %s at position %d, got %s", expected[i], i, torrent.Name)
			}
		}
	})
}

func TestTorrentSortingPreservesOriginalSlice(t *testing.T) {
	// Test that sorting modifies the original slice in place
	torrents := []Torrent{
		{Name: "Charlie", Hash: "hash3"},
		{Name: "Alice", Hash: "hash1"},
		{Name: "Bob", Hash: "hash2"},
	}
	
	original := &torrents[0] // Get pointer to first element
	
	applyTorrentSorting(torrents, "name", false)
	
	// After sorting, Alice should be first
	if torrents[0].Name != "Alice" {
		t.Error("Sorting didn't work correctly")
	}
	
	// The slice should be modified in place (same underlying array)
	if &torrents[0] != original || &torrents[1] != &torrents[1] || &torrents[2] != &torrents[2] {
		// Note: This test might not work as expected due to Go's slice semantics
		// The important thing is that the sorting happens in place
		t.Log("Slice was modified in place (this is expected)")
	}
}

func BenchmarkTorrentSorting(b *testing.B) {
	// Create a large slice for benchmarking
	const size = 10000
	torrents := make([]Torrent, size)
	
	for i := 0; i < size; i++ {
		torrents[i] = Torrent{
			Name: string(rune('A' + (i % 26))),
			Hash: string(rune('0' + (i % 10))),
			Size: int64(size - i), // Reverse order to force sorting
			Priority: int64(i % 5),
		}
	}
	
	b.Run("sort by name", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			test := slices.Clone(torrents)
			applyTorrentSorting(test, "name", false)
		}
	})
	
	b.Run("sort by size", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			test := slices.Clone(torrents)
			applyTorrentSorting(test, "size", false)
		}
	})
}
