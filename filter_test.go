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
