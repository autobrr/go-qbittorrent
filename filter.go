//go:generate go run internal/codegen/generate_torrent_filter.go

package qbittorrent

import (
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

// matchesTorrentFilter checks if a torrent matches the given filter options
func matchesTorrentFilter(torrent Torrent, options TorrentFilterOptions) bool {
	if len(options.Hashes) > 0 {
		found := false
		for _, h := range options.Hashes {
			if h == torrent.Hash {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if options.Category != "" && torrent.Category != options.Category {
		return false
	}
	if options.Tag != "" && !strings.Contains(torrent.Tags, options.Tag) {
		return false
	}
	if options.Filter != "" && !matchesStateFilter(torrent.State, options.Filter) {
		return false
	}
	return true
}

// stateFilterMatches is a precomputed lookup table for state-filter matches
var stateFilterMatches = map[TorrentState]map[TorrentFilter]struct{}{
	TorrentStateError: {
		TorrentFilterAll:      struct{}{},
		TorrentFilterError:    struct{}{},
		TorrentFilterInactive: struct{}{},
	},
	TorrentStateMissingFiles: {
		TorrentFilterAll:      struct{}{},
		TorrentFilterInactive: struct{}{},
	},
	TorrentStateUploading: {
		TorrentFilterAll:       struct{}{},
		TorrentFilterActive:    struct{}{},
		TorrentFilterUploading: struct{}{},
		TorrentFilterCompleted: struct{}{},
		TorrentFilterResumed:   struct{}{},
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
		TorrentFilterCompleted: struct{}{},
		TorrentFilterInactive:  struct{}{},
	},
	TorrentStateStalledUp: {
		TorrentFilterAll:              struct{}{},
		TorrentFilterStalled:          struct{}{},
		TorrentFilterStalledUploading: struct{}{},
		TorrentFilterCompleted:        struct{}{},
		TorrentFilterInactive:         struct{}{},
	},
	TorrentStateCheckingUp: {
		TorrentFilterAll:       struct{}{},
		TorrentFilterActive:    struct{}{},
		TorrentFilterCompleted: struct{}{},
		TorrentFilterResumed:   struct{}{},
	},
	TorrentStateForcedUp: {
		TorrentFilterAll:       struct{}{},
		TorrentFilterActive:    struct{}{},
		TorrentFilterUploading: struct{}{},
		TorrentFilterCompleted: struct{}{},
		TorrentFilterResumed:   struct{}{},
	},
	TorrentStateAllocating: {
		TorrentFilterAll:         struct{}{},
		TorrentFilterActive:      struct{}{},
		TorrentFilterDownloading: struct{}{},
		TorrentFilterResumed:     struct{}{},
	},
	TorrentStateDownloading: {
		TorrentFilterAll:         struct{}{},
		TorrentFilterActive:      struct{}{},
		TorrentFilterDownloading: struct{}{},
		TorrentFilterResumed:     struct{}{},
	},
	TorrentStateMetaDl: {
		TorrentFilterAll:         struct{}{},
		TorrentFilterActive:      struct{}{},
		TorrentFilterDownloading: struct{}{},
		TorrentFilterResumed:     struct{}{},
	},
	TorrentStatePausedDl: {
		TorrentFilterAll:      struct{}{},
		TorrentFilterPaused:   struct{}{},
		TorrentFilterStopped:  struct{}{},
		TorrentFilterInactive: struct{}{},
	},
	TorrentStateStoppedDl: {
		TorrentFilterAll:      struct{}{},
		TorrentFilterPaused:   struct{}{},
		TorrentFilterStopped:  struct{}{},
		TorrentFilterInactive: struct{}{},
	},
	TorrentStateQueuedDl: {
		TorrentFilterAll:      struct{}{},
		TorrentFilterInactive: struct{}{},
	},
	TorrentStateStalledDl: {
		TorrentFilterAll:                struct{}{},
		TorrentFilterStalled:            struct{}{},
		TorrentFilterStalledDownloading: struct{}{},
		TorrentFilterInactive:           struct{}{},
	},
	TorrentStateCheckingDl: {
		TorrentFilterAll:         struct{}{},
		TorrentFilterActive:      struct{}{},
		TorrentFilterDownloading: struct{}{},
		TorrentFilterResumed:     struct{}{},
	},
	TorrentStateForcedDl: {
		TorrentFilterAll:         struct{}{},
		TorrentFilterActive:      struct{}{},
		TorrentFilterDownloading: struct{}{},
		TorrentFilterResumed:     struct{}{},
	},
	TorrentStateCheckingResumeData: {
		TorrentFilterAll: struct{}{},
	},
	TorrentStateMoving: {
		TorrentFilterAll: struct{}{},
	},
	TorrentStateUnknown: {
		TorrentFilterAll: struct{}{},
	},
}

// matchesStateFilter checks if a torrent state matches the given filter using precomputed lookup
func matchesStateFilter(state TorrentState, filter TorrentFilter) bool {
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
