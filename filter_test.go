package qbittorrent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

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

func TestKnownTorrentStatesMatchesDeclaredConstants(t *testing.T) {
	t.Parallel()

	declared := declaredTorrentStates(t)
	if len(declared) == 0 {
		t.Fatal("found no TorrentState constants")
	}

	known := make(map[TorrentState]struct{})
	for _, state := range KnownTorrentStates() {
		known[state] = struct{}{}
	}

	for name, state := range declared {
		if !state.IsKnown() {
			t.Errorf("%s (%q) is declared but missing from stateFilterMatches", name, state)
		}
		delete(known, state)
	}
	for state := range known {
		t.Errorf("stateFilterMatches has %q, which is not a declared TorrentState constant", state)
	}
}

func TestKnownTorrentStates(t *testing.T) {
	t.Parallel()

	states := KnownTorrentStates()
	if !slices.IsSorted(states) {
		t.Fatalf("KnownTorrentStates() is not sorted: %v", states)
	}

	states[0] = "mutated"
	if KnownTorrentStates()[0] == "mutated" {
		t.Fatal("KnownTorrentStates() returned a shared slice")
	}
}

func TestTorrentStateIsKnown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state TorrentState
		want  bool
	}{
		{state: TorrentStateDownloading, want: true},
		{state: TorrentStateUnknown, want: true},
		{state: "", want: false},
		{state: "notAState", want: false},
		{state: "Downloading", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(string(tt.state), func(t *testing.T) {
			t.Parallel()
			if got := tt.state.IsKnown(); got != tt.want {
				t.Fatalf("TorrentState(%q).IsKnown() = %v, want %v", tt.state, got, tt.want)
			}
		})
	}
}

// declaredTorrentStates parses the package's non-test source files and returns
// every constant of type TorrentState, keyed by name.
func declaredTorrentStates(t *testing.T) map[string]TorrentState {
	t.Helper()

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob source files: %v", err)
	}

	fset := token.NewFileSet()
	declared := make(map[string]TorrentState)
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)
				typ, ok := vs.Type.(*ast.Ident)
				if !ok || typ.Name != "TorrentState" {
					continue
				}
				if len(vs.Values) != len(vs.Names) {
					t.Fatalf("%s: TorrentState constant %s must have an explicit value", fset.Position(vs.Pos()), vs.Names[0].Name)
				}
				for i, name := range vs.Names {
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						t.Fatalf("%s: TorrentState constant %s must be a string literal", fset.Position(vs.Pos()), name.Name)
					}
					value, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatalf("%s: unquote %s: %v", fset.Position(lit.Pos()), lit.Value, err)
					}
					declared[name.Name] = TorrentState(value)
				}
			}
		}
	}

	return declared
}
