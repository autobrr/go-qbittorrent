package qbittorrent

import "testing"

func TestHasExactTag(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		tag    string
		expect bool
	}{
		{name: "matches exact tag", input: "bc,abcd,abcde", tag: "bc", expect: true},
		{name: "ignores prefix", input: "abcd,abcde", tag: "bc", expect: false},
		{name: "trims whitespace", input: "tag1, tag2 , tag3", tag: "tag2", expect: true},
		{name: "keeps internal spaces", input: "tag 1,tag 2", tag: "tag 2", expect: true},
		{name: "matches spaced tag with surrounding whitespace", input: "  spaced tag  ,other", tag: "spaced tag", expect: true},
		{name: "empty input", input: "", tag: "tag", expect: false},
		{name: "empty target", input: "tag1,tag2", tag: "", expect: false},
	}

	for _, tt := range tests {
		tc := tt
		t.Run(tc.name, func(t *testing.T) {
			if got := hasExactTag(tc.input, tc.tag); got != tc.expect {
				t.Fatalf("hasExactTag(%q, %q) = %v, expect %v", tc.input, tc.tag, got, tc.expect)
			}
		})
	}
}

func TestMatchesTorrentFilter_Tag(t *testing.T) {
	torrent := Torrent{Tags: "bc,abcd"}

	if !matchesTorrentFilter(torrent, TorrentFilterOptions{Tag: "bc"}) {
		t.Fatalf("expected torrent to match tag 'bc'")
	}

	if matchesTorrentFilter(torrent, TorrentFilterOptions{Tag: "b"}) {
		t.Fatalf("expected torrent not to match partial tag 'b'")
	}
}
