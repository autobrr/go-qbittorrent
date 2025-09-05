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

	seen := make(map[string]bool)
	result := make([]string, 0, len(input))

	for _, item := range input {
		if !seen[item] {
			seen[item] = true
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

	removeMap := make(map[string]bool)
	for _, item := range toRemove {
		removeMap[item] = true
	}

	result := make([]string, 0, len(input))
	for _, item := range input {
		if !removeMap[item] {
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
var stateFilterMatches = map[TorrentState]map[TorrentFilter]bool{
	TorrentStateError: {
		TorrentFilterAll:      true,
		TorrentFilterError:    true,
		TorrentFilterInactive: true,
	},
	TorrentStateMissingFiles: {
		TorrentFilterAll:      true,
		TorrentFilterInactive: true,
	},
	TorrentStateUploading: {
		TorrentFilterAll:       true,
		TorrentFilterActive:    true,
		TorrentFilterUploading: true,
		TorrentFilterCompleted: true,
		TorrentFilterResumed:   true,
	},
	TorrentStatePausedUp: {
		TorrentFilterAll:       true,
		TorrentFilterPaused:    true,
		TorrentFilterStopped:   true,
		TorrentFilterCompleted: true,
		TorrentFilterInactive:  true,
	},
	TorrentStateStoppedUp: {
		TorrentFilterAll:       true,
		TorrentFilterPaused:    true,
		TorrentFilterStopped:   true,
		TorrentFilterCompleted: true,
		TorrentFilterInactive:  true,
	},
	TorrentStateQueuedUp: {
		TorrentFilterAll:       true,
		TorrentFilterCompleted: true,
		TorrentFilterInactive:  true,
	},
	TorrentStateStalledUp: {
		TorrentFilterAll:              true,
		TorrentFilterStalled:          true,
		TorrentFilterStalledUploading: true,
		TorrentFilterCompleted:        true,
		TorrentFilterInactive:         true,
	},
	TorrentStateCheckingUp: {
		TorrentFilterAll:       true,
		TorrentFilterActive:    true,
		TorrentFilterCompleted: true,
		TorrentFilterResumed:   true,
	},
	TorrentStateForcedUp: {
		TorrentFilterAll:       true,
		TorrentFilterActive:    true,
		TorrentFilterUploading: true,
		TorrentFilterCompleted: true,
		TorrentFilterResumed:   true,
	},
	TorrentStateAllocating: {
		TorrentFilterAll:         true,
		TorrentFilterActive:      true,
		TorrentFilterDownloading: true,
		TorrentFilterResumed:     true,
	},
	TorrentStateDownloading: {
		TorrentFilterAll:         true,
		TorrentFilterActive:      true,
		TorrentFilterDownloading: true,
		TorrentFilterResumed:     true,
	},
	TorrentStateMetaDl: {
		TorrentFilterAll:         true,
		TorrentFilterActive:      true,
		TorrentFilterDownloading: true,
		TorrentFilterResumed:     true,
	},
	TorrentStatePausedDl: {
		TorrentFilterAll:      true,
		TorrentFilterPaused:   true,
		TorrentFilterStopped:  true,
		TorrentFilterInactive: true,
	},
	TorrentStateStoppedDl: {
		TorrentFilterAll:      true,
		TorrentFilterPaused:   true,
		TorrentFilterStopped:  true,
		TorrentFilterInactive: true,
	},
	TorrentStateQueuedDl: {
		TorrentFilterAll:      true,
		TorrentFilterInactive: true,
	},
	TorrentStateStalledDl: {
		TorrentFilterAll:                true,
		TorrentFilterStalled:            true,
		TorrentFilterStalledDownloading: true,
		TorrentFilterInactive:           true,
	},
	TorrentStateCheckingDl: {
		TorrentFilterAll:         true,
		TorrentFilterActive:      true,
		TorrentFilterDownloading: true,
		TorrentFilterResumed:     true,
	},
	TorrentStateForcedDl: {
		TorrentFilterAll:         true,
		TorrentFilterActive:      true,
		TorrentFilterDownloading: true,
		TorrentFilterResumed:     true,
	},
	TorrentStateCheckingResumeData: {
		TorrentFilterAll: true,
	},
	TorrentStateMoving: {
		TorrentFilterAll: true,
	},
	TorrentStateUnknown: {
		TorrentFilterAll: true,
	},
}

// matchesStateFilter checks if a torrent state matches the given filter using precomputed lookup
func matchesStateFilter(state TorrentState, filter TorrentFilter) bool {
	if stateMap, exists := stateFilterMatches[state]; exists {
		return stateMap[filter]
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
