package qbittorrent

import "testing"

func TestContainsExactTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		tags   string
		target string
		want   bool
	}{
		{name: "exact match", tags: "foo,bar", target: "foo", want: true},
		{name: "trimmed match", tags: "foo, bar", target: "bar", want: true},
		{name: "no match", tags: "foo,bar", target: "baz", want: false},
		{name: "substring not match", tags: "foobar,baz", target: "foo", want: false},
		{name: "empty tags", tags: "", target: "foo", want: false},
		{name: "blank target", tags: "foo,bar", target: " ", want: false},
		{name: "ab exact match", tags: "a,ab,abc", target: "ab", want: true},
		{name: "abc exact match", tags: "a,ab,abc", target: "abc", want: true},
		{name: "ab not substring of abc", tags: "abc,def", target: "ab", want: false},
		{name: "abc not superstring of ab", tags: "ab,def", target: "abc", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := containsExactTag(tt.tags, tt.target); got != tt.want {
				t.Fatalf("containsExactTag(%q, %q) = %v, want %v", tt.tags, tt.target, got, tt.want)
			}
		})
	}
}

func TestMatchesTorrentFilter_Tag(t *testing.T) {
	t.Parallel()

	torrent := Torrent{Tags: "alpha, beta"}

	tests := []struct {
		name    string
		options TorrentFilterOptions
		want    bool
	}{
		{name: "match", options: TorrentFilterOptions{Tag: "alpha"}, want: true},
		{name: "substring not match", options: TorrentFilterOptions{Tag: "alp"}, want: false},
		{name: "trim spaces", options: TorrentFilterOptions{Tag: "beta"}, want: true},
		{name: "no tag filter", options: TorrentFilterOptions{}, want: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := matchesTorrentFilter(torrent, tt.options); got != tt.want {
				t.Fatalf("matchesTorrentFilter() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchesStateFilter_ForcedMetaDl(t *testing.T) {
	t.Parallel()

	tests := []struct {
		filter TorrentFilter
		want   bool
	}{
		{filter: TorrentFilterAll, want: true},
		{filter: TorrentFilterActive, want: true},
		{filter: TorrentFilterDownloading, want: true},
		{filter: TorrentFilterResumed, want: true},
		{filter: TorrentFilterRunning, want: true},
		{filter: TorrentFilterInactive, want: false},
		{filter: TorrentFilterCompleted, want: false},
		{filter: TorrentFilterPaused, want: false},
		{filter: TorrentFilterStopped, want: false},
		{filter: TorrentFilterStalled, want: false},
		{filter: TorrentFilterStalledDownloading, want: false},
		{filter: TorrentFilterUploading, want: false},
		{filter: TorrentFilterError, want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(string(tt.filter), func(t *testing.T) {
			t.Parallel()
			if got := matchesStateFilter(TorrentStateForcedMetaDl, tt.filter); got != tt.want {
				t.Fatalf("matchesStateFilter(%q, %q) = %v, want %v", TorrentStateForcedMetaDl, tt.filter, got, tt.want)
			}
			if got := matchesStateFilter(TorrentStateMetaDl, tt.filter); got != tt.want {
				t.Fatalf("matchesStateFilter(%q, %q) = %v, want %v", TorrentStateMetaDl, tt.filter, got, tt.want)
			}
		})
	}
}
