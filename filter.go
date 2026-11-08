//go:generate go run internal/codegen/generate_torrent_filter.go

package qbittorrent

import (
	"maps"
	"slices"
	"strings"
)

// removeDuplicateStrings removes duplicate strings from a slice and returns unique items
func removeDuplicateStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(input))
	result := make([]string, 0, len(input))

	for _, item := range input {
		if _, ok := seen[item]; !ok {
			seen[item] = struct{}{}
			result = append(result, item)
		}
	}

	return result
}

// removeStrings removes specified strings from a slice and returns the remaining items
func removeStrings(input []string, toRemove []string) []string {
	if len(input) == 0 || len(toRemove) == 0 {
		return input
	}

	removeMap := make(map[string]struct{}, len(toRemove))
	for _, item := range toRemove {
		removeMap[item] = struct{}{}
	}

	result := make([]string, 0, len(input))
	for _, item := range input {
		if _, ok := removeMap[item]; !ok {
			result = append(result, item)
		}
	}

	return result
}

// matchesTorrentFilter checks if a torrent matches the category, tag and state
// filters. The caller applies the hash filter.
func matchesTorrentFilter(torrent Torrent, options TorrentFilterOptions) bool {
	if options.Category != "" && torrent.Category != options.Category {
		return false
	}
	if options.Tag != "" && !containsExactTag(torrent.Tags, options.Tag) {
		return false
	}
	if options.Filter != "" && !matchesStateFilter(torrent.State, options.Filter) {
		return false
	}
	return true
}

// containsExactTag reports whether the comma-separated tags string includes the target as a full token.
func containsExactTag(tags string, target string) bool {
	if tags == "" || target == "" {
		return false
	}

	trimmed := strings.TrimSpace(target)
	if trimmed == "" {
		return false
	}

	for tag := range strings.SplitSeq(tags, ",") {
		if strings.TrimSpace(tag) == trimmed {
			return true
		}
	}

	return false
}

// stateFilterMatches is a precomputed lookup table for state-filter matches.
// Its keys are the known states returned by KnownTorrentStates, so every
// TorrentState constant needs an entry here. testdata/filters holds the
// results of real qBittorrent that the table follows.
var stateFilterMatches = map[TorrentState]map[TorrentFilter]struct{}{
	TorrentStateError: {
		TorrentFilterAll:      struct{}{},
		TorrentFilterError:    struct{}{},
		TorrentFilterInactive: struct{}{},
	},
	TorrentStateMissingFiles: {
		TorrentFilterAll:      struct{}{},
		TorrentFilterError:    struct{}{},
		TorrentFilterInactive: struct{}{},
	},
	TorrentStateUploading: {
		TorrentFilterAll:       struct{}{},
		TorrentFilterActive:    struct{}{},
		TorrentFilterSeeding:   struct{}{},
		TorrentFilterCompleted: struct{}{},
	},
	TorrentStatePausedUp: {
		TorrentFilterAll:       struct{}{},
		TorrentFilterPaused:    struct{}{},
		TorrentFilterStopped:   struct{}{},
		TorrentFilterCompleted: struct{}{},
		TorrentFilterInactive:  struct{}{},
	},
	TorrentStateStoppedUp: {
		TorrentFilterAll:       struct{}{},
		TorrentFilterPaused:    struct{}{},
		TorrentFilterStopped:   struct{}{},
		TorrentFilterCompleted: struct{}{},
		TorrentFilterInactive:  struct{}{},
	},
	TorrentStateQueuedUp: {
		TorrentFilterAll:       struct{}{},
		TorrentFilterSeeding:   struct{}{},
		TorrentFilterCompleted: struct{}{},
		TorrentFilterInactive:  struct{}{},
	},
	TorrentStateStalledUp: {
		TorrentFilterAll:              struct{}{},
		TorrentFilterStalled:          struct{}{},
		TorrentFilterStalledUploading: struct{}{},
		TorrentFilterSeeding:          struct{}{},
		TorrentFilterCompleted:        struct{}{},
		TorrentFilterInactive:         struct{}{},
	},
	TorrentStateCheckingUp: {
		TorrentFilterAll:       struct{}{},
		TorrentFilterSeeding:   struct{}{},
		TorrentFilterCompleted: struct{}{},
		TorrentFilterChecking:  struct{}{},
		TorrentFilterInactive:  struct{}{},
	},
	TorrentStateForcedUp: {
		TorrentFilterAll:       struct{}{},
		TorrentFilterActive:    struct{}{},
		TorrentFilterSeeding:   struct{}{},
		TorrentFilterCompleted: struct{}{},
	},
	TorrentStateAllocating: {
		TorrentFilterAll:         struct{}{},
		TorrentFilterInactive:    struct{}{},
		TorrentFilterDownloading: struct{}{},
	},
	TorrentStateDownloading: {
		TorrentFilterAll:         struct{}{},
		TorrentFilterActive:      struct{}{},
		TorrentFilterDownloading: struct{}{},
	},
	TorrentStateMetaDl: {
		TorrentFilterAll:         struct{}{},
		TorrentFilterActive:      struct{}{},
		TorrentFilterDownloading: struct{}{},
	},
	TorrentStateForcedMetaDl: {
		TorrentFilterAll:         struct{}{},
		TorrentFilterActive:      struct{}{},
		TorrentFilterDownloading: struct{}{},
	},
	TorrentStatePausedDl: {
		TorrentFilterAll:         struct{}{},
		TorrentFilterPaused:      struct{}{},
		TorrentFilterStopped:     struct{}{},
		TorrentFilterDownloading: struct{}{},
		TorrentFilterInactive:    struct{}{},
	},
	TorrentStateStoppedDl: {
		TorrentFilterAll:         struct{}{},
		TorrentFilterPaused:      struct{}{},
		TorrentFilterStopped:     struct{}{},
		TorrentFilterDownloading: struct{}{},
		TorrentFilterInactive:    struct{}{},
	},
	TorrentStateQueuedDl: {
		TorrentFilterAll:         struct{}{},
		TorrentFilterDownloading: struct{}{},
		TorrentFilterInactive:    struct{}{},
	},
	TorrentStateStalledDl: {
		TorrentFilterAll:                struct{}{},
		TorrentFilterStalled:            struct{}{},
		TorrentFilterStalledDownloading: struct{}{},
		TorrentFilterDownloading:        struct{}{},
		TorrentFilterInactive:           struct{}{},
	},
	TorrentStateCheckingDl: {
		TorrentFilterAll:         struct{}{},
		TorrentFilterDownloading: struct{}{},
		TorrentFilterChecking:    struct{}{},
		TorrentFilterInactive:    struct{}{},
	},
	TorrentStateForcedDl: {
		TorrentFilterAll:         struct{}{},
		TorrentFilterActive:      struct{}{},
		TorrentFilterDownloading: struct{}{},
	},
	TorrentStateCheckingResumeData: {
		TorrentFilterAll:      struct{}{},
		TorrentFilterChecking: struct{}{},
		TorrentFilterInactive: struct{}{},
	},
	TorrentStateMoving: {
		TorrentFilterAll:    struct{}{},
		TorrentFilterActive: struct{}{},
		TorrentFilterMoving: struct{}{},
	},
	TorrentStateUnknown: {
		TorrentFilterAll: struct{}{},
	},
}

// IsKnown reports whether s is a TorrentState declared by this library.
// It does not validate: unknown values are still unmarshalled verbatim.
func (s TorrentState) IsKnown() bool {
	_, ok := stateFilterMatches[s]
	return ok
}

// KnownTorrentStates returns every TorrentState declared by this library, sorted.
// The caller owns the returned slice.
func KnownTorrentStates() []TorrentState {
	return slices.Sorted(maps.Keys(stateFilterMatches))
}

// matchesStateFilter checks if a torrent state matches the given filter using precomputed lookup
func matchesStateFilter(state TorrentState, filter TorrentFilter) bool {
	if filter == TorrentFilterUploading {
		filter = TorrentFilterSeeding
	}
	// running and resumed are the same filter: qBittorrent 5.0.0 renamed resumed.
	if filter == TorrentFilterRunning || filter == TorrentFilterResumed {
		return state != TorrentStatePausedUp && state != TorrentStatePausedDl &&
			state != TorrentStateStoppedUp && state != TorrentStateStoppedDl
	}

	if stateMap, exists := stateFilterMatches[state]; exists {
		_, ok := stateMap[filter]
		return ok
	}
	return filter == TorrentFilterAll
}

// applyTorrentFilterOptions applies sorting, reverse, limit, and offset to torrents
func applyTorrentFilterOptions(torrents []Torrent, options TorrentFilterOptions) []Torrent {
	// Sort
	applyTorrentSorting(torrents, options.Sort, options.Reverse)

	// Apply offset and limit
	if options.Offset > 0 || options.Limit > 0 {
		start := options.Offset
		if start >= len(torrents) {
			torrents = torrents[:0]
		} else {
			end := len(torrents)
			if options.Limit > 0 && start+options.Limit < end {
				end = start + options.Limit
			}
			torrents = torrents[start:end]
		}
	}

	return torrents
}
