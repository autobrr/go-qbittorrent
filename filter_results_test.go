package qbittorrent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/require"
)

// filterCapture is a file that `go run . filters` in internal/capture wrote.
type filterCapture struct {
	Version              string                       `json:"version"`
	Filters              []string                     `json:"filters"`
	ReturnedEveryTorrent []string                     `json:"returned_every_torrent"`
	Verdicts             map[string]map[string]string `json:"verdicts"`
	Categories           []struct {
		Value         string `json:"value"`
		Subcategories bool   `json:"subcategories"`
		Torrents      []struct {
			Category string `json:"category"`
			Returned bool   `json:"returned"`
		} `json:"torrents"`
	} `json:"categories"`
	Tags []struct {
		Value         string `json:"value"`
		Subcategories bool   `json:"subcategories"`
		Torrents      []struct {
			Tags     string `json:"tags"`
			Returned bool   `json:"returned"`
		} `json:"torrents"`
	} `json:"tags"`
}

// libraryFilters are the filter names that the library applies.
var libraryFilters = []TorrentFilter{TorrentFilterAll, TorrentFilterActive, TorrentFilterInactive,
	TorrentFilterCompleted, TorrentFilterResumed, TorrentFilterPaused, TorrentFilterStopped, TorrentFilterRunning,
	TorrentFilterStalled, TorrentFilterSeeding, TorrentFilterUploading, TorrentFilterStalledUploading,
	TorrentFilterDownloading, TorrentFilterStalledDownloading, TorrentFilterError, TorrentFilterChecking,
	TorrentFilterMoving}

// A stateCase is one recorded verdict that the library does not give.
type stateCase struct {
	capture *filterCapture
	filter  string
	state   string
	verdict string // the verdict of qBittorrent: yes, no, or mixed
}

func (c stateCase) acceptedByRelease() bool {
	return !slices.Contains(c.capture.ReturnedEveryTorrent, c.filter)
}

func (c stateCase) since(v string) bool {
	return semver.MustParse(strings.TrimPrefix(c.capture.Version, "v")).
		GreaterThanEqual(semver.MustParse(v))
}

// stateDifferences are the named differences between the library filter and
// qBittorrent. A difference that is not listed fails the test.
type stateDifference struct {
	name, reason string
	applies      func(stateCase) bool
}

var stateDifferences = []stateDifference{
	{
		"each filter name works on each version",
		"qBittorrent returns every torrent for a filter name that the release does not accept, " +
			"and the library applies each of its filters on each version",
		func(c stateCase) bool {
			return !c.acceptedByRelease() && c.filter != string(TorrentFilterUploading) &&
				slices.Contains(libraryFilters, TorrentFilter(c.filter))
		},
	},
	{
		"uploading means seeding",
		"qBittorrent has no uploading filter and returns every torrent, " +
			"but TorrentFilterUploading is the same as TorrentFilterSeeding",
		func(c stateCase) bool { return c.filter == string(TorrentFilterUploading) && !c.acceptedByRelease() },
	},
	{
		"an unknown filter value gives no rows",
		"qBittorrent returns every torrent for a filter value that it does not know, " +
			"and the library returns no torrent",
		func(c stateCase) bool {
			return !c.acceptedByRelease() && !slices.Contains(libraryFilters, TorrentFilter(c.filter)) && c.verdict == "yes"
		},
	},
	{
		"a stopped torrent in checkingResumeData, error, or missingFiles",
		"the state hides if the torrent is stopped, so one capture run can differ from the next",
		func(c stateCase) bool {
			if !slices.Contains([]string{"checkingResumeData", "error", "missingFiles"}, c.state) {
				return false
			}
			switch c.filter {
			case "stopped", "paused":
				return c.verdict == "yes" || c.verdict == "mixed"
			case "running", "resumed":
				return c.verdict == "no" || c.verdict == "mixed"
			}
			return false
		},
	},
	{
		"pausedUP and stoppedUP with a mixed verdict in a running filter",
		"a torrent that has just stopped can still count as running for a moment",
		func(c stateCase) bool {
			return (c.state == "pausedUP" || c.state == "stoppedUP") &&
				(c.filter == "running" || c.filter == "resumed") && c.verdict == "mixed"
		},
	},
	{
		"checkingUP with a mixed verdict in the resumed and paused filters on 4.3.9",
		"4.3.9 has no running or stopped filter, and a recheck of a stopped torrent shows checkingUP " +
			"while the torrent is stopped",
		func(c stateCase) bool {
			return c.capture.Version == "v4.3.9" && c.state == "checkingUP" &&
				(c.filter == "resumed" || c.filter == "paused") && c.verdict == "mixed"
		},
	},
	{
		"active and inactive by state",
		"from 5.2, qBittorrent uses the transfer rate for active and inactive, " +
			"and the library uses the state list of 5.1.4 on each version",
		func(c stateCase) bool { return (c.filter == "active" || c.filter == "inactive") && c.since("5.2.0") },
	},
}

func loadFilterCaptures(t *testing.T) map[string]*filterCapture {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", "filters", "*.json"))
	require.NoError(t, err)
	require.Len(t, files, 7, "one capture for each release from 4.3.9 to 5.3.0")
	captures := map[string]*filterCapture{}
	for _, file := range files {
		body, err := os.ReadFile(file)
		require.NoError(t, err)
		var c filterCapture
		require.NoError(t, json.Unmarshal(body, &c))
		require.Equal(t, "v"+strings.TrimSuffix(filepath.Base(file), ".json"), c.Version)
		captures[filepath.Base(file)] = &c
	}
	return captures
}

// TestFilterResultsMatchQBittorrent compares the sync layer filter with the
// recorded results of real qBittorrent releases.
func TestFilterResultsMatchQBittorrent(t *testing.T) {
	var torrents []Torrent
	for _, state := range KnownTorrentStates() {
		torrents = append(torrents, Torrent{Hash: string(state), State: state})
	}
	sm := newSyncedManager(t, torrents...)

	for file, capture := range loadFilterCaptures(t) {
		t.Run(file, func(t *testing.T) {
			for _, state := range KnownTorrentStates() {
				require.Contains(t, capture.Filters, string(state), "no result for the filter value %q", state)
				require.NotEmpty(t, capture.Verdicts[string(state)], "no result for the filter value %q", state)
			}
			for filter, states := range capture.Verdicts {
				matched := hashes(sm.GetTorrentsUnchecked(TorrentFilterOptions{Filter: TorrentFilter(filter)}))
				for state, verdict := range states {
					want := "no"
					if slices.Contains(matched, state) {
						want = "yes"
					}
					if verdict == want {
						continue
					}
					c := stateCase{capture, filter, state, verdict}
					if !slices.ContainsFunc(stateDifferences, func(d stateDifference) bool { return d.applies(c) }) {
						t.Errorf("%s: filter %q, state %q: qBittorrent says %s, the library says %s",
							capture.Version, filter, state, verdict, want)
					}
				}
			}
		})
	}
}

// TestCategoryAndTagResultsMatchQBittorrent compares the Category and Tag
// options with the recorded results. The named differences:
//   - An empty Category or Tag option means no filter, because the field has
//     no separate unset value. Real category= and tag= give the torrents with
//     no category or no tag.
//   - The Category option is an exact match on each version, also when
//     qBittorrent uses subcategories. Cross-seed needs the exact match.
//   - The Tag option is an exact match on each version. 4.3.9 has no tag
//     parameter, so it returns every torrent for each tag value.
func TestCategoryAndTagResultsMatchQBittorrent(t *testing.T) {
	for file, capture := range loadFilterCaptures(t) {
		t.Run(file, func(t *testing.T) {
			require.NotEmpty(t, capture.Categories)
			require.NotEmpty(t, capture.Tags)
			for _, c := range capture.Categories {
				var torrents []Torrent
				for i, p := range c.Torrents {
					torrents = append(torrents, Torrent{Hash: strconv.Itoa(i), Category: p.Category})
				}
				matched := hashes(newSyncedManager(t, torrents...).GetTorrentsUnchecked(TorrentFilterOptions{Category: c.Value}))
				for i, p := range c.Torrents {
					lib := slices.Contains(matched, strconv.Itoa(i))
					if lib == p.Returned || c.Value == "" ||
						c.Subcategories && p.Returned && strings.HasPrefix(p.Category, c.Value+"/") {
						continue
					}
					t.Errorf("%s: category %q (subcategories %t), torrent in %q: qBittorrent returned it: %t, the library: %t",
						capture.Version, c.Value, c.Subcategories, p.Category, p.Returned, lib)
				}
			}
			for _, c := range capture.Tags {
				var torrents []Torrent
				for i, p := range c.Torrents {
					torrents = append(torrents, Torrent{Hash: strconv.Itoa(i), Tags: p.Tags})
				}
				matched := hashes(newSyncedManager(t, torrents...).GetTorrentsUnchecked(TorrentFilterOptions{Tag: c.Value}))
				for i, p := range c.Torrents {
					lib := slices.Contains(matched, strconv.Itoa(i))
					if lib == p.Returned || c.Value == "" || capture.Version == "v4.3.9" && p.Returned {
						continue
					}
					t.Errorf("%s: tag %q, torrent with tags %q: qBittorrent returned it: %t, the library: %t",
						capture.Version, c.Value, p.Tags, p.Returned, lib)
				}
			}
		})
	}
}
