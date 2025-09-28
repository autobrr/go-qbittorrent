package qbittorrent

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

type liveDataset struct {
	torrents []Torrent
	rawTags  []string
	targets  []string
}

var (
	liveDatasetOnce sync.Once
	liveData        *liveDataset
	liveDataErr     error
)


func loadLiveDataset() (*liveDataset, error) {
	liveDatasetOnce.Do(func() {
		host := os.Getenv("QBIT_BENCH_HOST")
		if host == "" {
			liveDataErr = fmt.Errorf("QBIT_BENCH_HOST environment variable is required")
			return
		}
		username := os.Getenv("QBIT_BENCH_USER")
		if username == "" {
			liveDataErr = fmt.Errorf("QBIT_BENCH_USER environment variable is required")
			return
		}
		password := os.Getenv("QBIT_BENCH_PASS")
		if password == "" {
			liveDataErr = fmt.Errorf("QBIT_BENCH_PASS environment variable is required")
			return
		}

		cfg := Config{
			Host:     host,
			Username: username,
			Password: password,
		}

		client := NewClient(cfg)

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		if err := client.LoginCtx(ctx); err != nil {
			liveDataErr = fmt.Errorf("login failed: %w", err)
			return
		}

		mainData, err := client.SyncMainDataCtx(ctx, 0)
		if err != nil {
			liveDataErr = fmt.Errorf("sync main data failed: %w", err)
			return
		}

		if mainData == nil {
			liveDataErr = fmt.Errorf("received nil main data from server")
			return
		}

		dataset := &liveDataset{}
		targetSet := make(map[string]struct{})

		for _, torrent := range mainData.Torrents {
			dataset.torrents = append(dataset.torrents, torrent)
			if torrent.Tags == "" {
				continue
			}

			dataset.rawTags = append(dataset.rawTags, torrent.Tags)

			for _, token := range strings.Split(torrent.Tags, ",") {
				trimmed := strings.TrimSpace(token)
				if trimmed == "" {
					continue
				}
				if _, ok := targetSet[trimmed]; !ok {
					targetSet[trimmed] = struct{}{}
					dataset.targets = append(dataset.targets, trimmed)
				}
			}
		}

		if len(dataset.targets) == 0 {
			liveDataErr = fmt.Errorf("no tagged torrents returned by %s", cfg.Host)
			return
		}

		sort.Strings(dataset.targets)
		liveData = dataset
	})

	return liveData, liveDataErr
}


func requireLiveDataset(tb testing.TB) *liveDataset {
	tb.Helper()

	dataset, err := loadLiveDataset()
	if err != nil {
		tb.Skipf("skipping live qBittorrent tests: %v", err)
	}

	return dataset
}

func TestLiveContainsExactTagMatchesTokens(t *testing.T) {
	dataset := requireLiveDataset(t)

	if len(dataset.targets) == 0 {
		t.Skip("no tags returned from live qBittorrent instance")
	}

	var checked int

	for _, torrent := range dataset.torrents {
		if torrent.Tags == "" {
			continue
		}

		tokenSet := make(map[string]struct{})
		for _, token := range strings.Split(torrent.Tags, ",") {
			trimmed := strings.TrimSpace(token)
			if trimmed == "" {
				continue
			}
			tokenSet[trimmed] = struct{}{}
		}

		if len(tokenSet) == 0 {
			continue
		}

		for _, candidate := range dataset.targets {
			_, want := tokenSet[candidate]
			got := containsExactTag(torrent.Tags, candidate)
			if got != want {
				t.Fatalf("containsExactTag mismatch for torrent %q: tags=%q candidate=%q want=%v got=%v", torrent.Name, torrent.Tags, candidate, want, got)
			}
			checked++
		}
	}

	if checked == 0 {
		t.Skip("no torrents with tags available to validate")
	}
}

var liveBenchSink int

// containsExactTagSplit uses strings.Split (allocates)
func containsExactTagSplit(tags string, target string) bool {
	if tags == "" || target == "" {
		return false
	}

	parts := strings.Split(tags, ",")
	for _, part := range parts {
		if strings.TrimSpace(part) == target {
			return true
		}
	}
	return false
}

// containsTagNoAlloc avoids allocations by manually parsing
func containsTagNoAlloc(tags string, target string) bool {
	if tags == "" || target == "" {
		return false
	}

	start := 0
	for i := 0; i <= len(tags); i++ {
		if i == len(tags) || tags[i] == ',' {
			// Trim spaces manually
			tagStart := start
			tagEnd := i

			// Trim leading spaces
			for tagStart < tagEnd && tags[tagStart] == ' ' {
				tagStart++
			}

			// Trim trailing spaces
			for tagEnd > tagStart && tags[tagEnd-1] == ' ' {
				tagEnd--
			}

			// Compare the trimmed tag
			if tagEnd-tagStart == len(target) {
				match := true
				for j := 0; j < len(target); j++ {
					if tags[tagStart+j] != target[j] {
						match = false
						break
					}
				}
				if match {
					return true
				}
			}

			start = i + 1
		}
	}
	return false
}

func BenchmarkContainsExactTagLiveSplitSeq(b *testing.B) {
	dataset := requireLiveDataset(b)

	if len(dataset.rawTags) == 0 || len(dataset.targets) == 0 {
		b.Skip("no tagged torrents available for benchmark")
	}

	benchmarkLiveContainsExactTag(b, containsExactTag, dataset.rawTags, dataset.targets)
}

func BenchmarkContainsExactTagLiveSplit(b *testing.B) {
	dataset := requireLiveDataset(b)

	if len(dataset.rawTags) == 0 || len(dataset.targets) == 0 {
		b.Skip("no tagged torrents available for benchmark")
	}

	benchmarkLiveContainsExactTag(b, containsExactTagSplit, dataset.rawTags, dataset.targets)
}

func BenchmarkContainsTagLiveNoAlloc(b *testing.B) {
	dataset := requireLiveDataset(b)

	if len(dataset.rawTags) == 0 || len(dataset.targets) == 0 {
		b.Skip("no tagged torrents available for benchmark")
	}

	benchmarkLiveContainsExactTag(b, containsTagNoAlloc, dataset.rawTags, dataset.targets)
}

func benchmarkLiveContainsExactTag(b *testing.B, matcher func(string, string) bool, tags []string, targets []string) {
	b.Helper()

	lengthTags := len(tags)
	lengthTargets := len(targets)

	var local int

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if matcher(tags[i%lengthTags], targets[i%lengthTargets]) {
			local++
		}
	}
	b.StopTimer()

	liveBenchSink = local
}
