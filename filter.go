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

// matchesStateFilter checks if a torrent state matches the given filter
func matchesStateFilter(state TorrentState, filter TorrentFilter) bool {
	switch filter {
	case TorrentFilterAll:
		return true
	case TorrentFilterDownloading:
		return state == TorrentStateDownloading || state == TorrentStateMetaDl || state == TorrentStateStalledDl || state == TorrentStateCheckingDl || state == TorrentStateForcedDl || state == TorrentStateAllocating || state == TorrentStateQueuedDl
	case TorrentFilterUploading:
		return state == TorrentStateUploading || state == TorrentStateStalledUp || state == TorrentStateCheckingUp || state == TorrentStateForcedUp || state == TorrentStateQueuedUp
	case TorrentFilterCompleted:
		return state == TorrentStatePausedUp || state == TorrentStateStoppedUp || state == TorrentStateQueuedUp || state == TorrentStateStalledUp || state == TorrentStateCheckingUp || state == TorrentStateForcedUp
	case TorrentFilterPaused, TorrentFilterStopped:
		return state == TorrentStatePausedDl || state == TorrentStatePausedUp || state == TorrentStateStoppedDl || state == TorrentStateStoppedUp
	case TorrentFilterActive:
		return state == TorrentStateDownloading || state == TorrentStateUploading || state == TorrentStateMetaDl || state == TorrentStateCheckingDl || state == TorrentStateCheckingUp || state == TorrentStateForcedDl || state == TorrentStateForcedUp || state == TorrentStateAllocating
	case TorrentFilterInactive:
		return state == TorrentStatePausedDl || state == TorrentStatePausedUp || state == TorrentStateStoppedDl || state == TorrentStateStoppedUp || state == TorrentStateQueuedDl || state == TorrentStateQueuedUp || state == TorrentStateStalledDl || state == TorrentStateStalledUp
	case TorrentFilterResumed:
		return state == TorrentStateDownloading || state == TorrentStateUploading || state == TorrentStateMetaDl || state == TorrentStateCheckingDl || state == TorrentStateCheckingUp || state == TorrentStateForcedDl || state == TorrentStateForcedUp || state == TorrentStateAllocating
	case TorrentFilterStalled:
		return state == TorrentStateStalledDl || state == TorrentStateStalledUp
	case TorrentFilterStalledDownloading:
		return state == TorrentStateStalledDl
	case TorrentFilterStalledUploading:
		return state == TorrentStateStalledUp
	default:
		return true
	}
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
