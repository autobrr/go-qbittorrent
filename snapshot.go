package qbittorrent

import (
	"encoding/binary"
	"fmt"
	"hash/maphash"
	"maps"
	"slices"
	"time"
)

// SyncSnapshot holds the torrents of one instance as one sync of a
// [SyncManager] left them. The manager never changes a published snapshot.
// Each read returns the torrents of the snapshot, not copies, and every reader
// of the sync shares them: a caller must not change a torrent that it read.
// Copy a torrent before you change it. A copy still shares HasMetadata and
// Trackers with the snapshot: replace those fields, never write through them.
type SyncSnapshot struct {
	number   uint64
	time     time.Time
	torrents []*Torrent
	index    map[string]int // position in torrents, keyed by hash
}

// Number returns the number of the sync that published the snapshot. Each
// snapshot of a manager has a higher number than the one before it.
func (s *SyncSnapshot) Number() uint64 {
	return s.number
}

// Time returns the time of the sync that published the snapshot.
func (s *SyncSnapshot) Time() time.Time {
	return s.time
}

// Len returns the number of torrents.
func (s *SyncSnapshot) Len() int {
	return len(s.torrents)
}

// Torrent returns the torrent with the given hash.
func (s *SyncSnapshot) Torrent(hash string) (*Torrent, bool) {
	i, ok := s.index[hash]
	if !ok {
		return nil, false
	}
	return s.torrents[i], true
}

// Torrents returns all torrents in the order of the snapshot. Two snapshots
// of a manager list the torrents that both hold in the same relative order,
// and a torrent that a sync added comes after the others. The caller owns the
// returned slice.
func (s *SyncSnapshot) Torrents() []*Torrent {
	return slices.Clone(s.torrents)
}

// Matching returns the torrents for which match returns true, in the order of
// the snapshot. It returns an empty slice when no torrent matches.
func (s *SyncSnapshot) Matching(match func(*Torrent) bool) []*Torrent {
	matches := []*Torrent{}
	for _, t := range s.torrents {
		if match(t) {
			matches = append(matches, t)
		}
	}
	return matches
}

// SyncUpdate is what [SyncOptions.OnSyncUpdate] gets after a sync.
// The callback must not change the torrents of Snapshot. It owns the other
// fields, except the hash slices of Trackers, which it must not change.
type SyncUpdate struct {
	// Snapshot is the Sync snapshot that the sync published.
	Snapshot *SyncSnapshot
	// Changes tells what the sync changed.
	Changes     MainDataChanges
	ServerState ServerState
	Categories  map[string]Category
	Tags        []string
	Trackers    map[string][]string
}

// filter returns the torrents that match options, sorted and paged as
// options says. It returns an empty slice when no torrent matches.
func (s *SyncSnapshot) filter(options TorrentFilterOptions) []*Torrent {
	var refs []*Torrent
	filter := newTorrentFilter(options)
	if len(options.Hashes) > 0 {
		// Visit only the requested torrents, in the order of the snapshot.
		positions := make([]int, 0, len(options.Hashes))
		for _, hash := range options.Hashes {
			if i, ok := s.index[hash]; ok {
				positions = append(positions, i)
			}
		}
		slices.Sort(positions)
		positions = slices.Compact(positions)
		refs = make([]*Torrent, 0, len(positions))
		for _, i := range positions {
			if filter.matches(s.torrents[i]) {
				refs = append(refs, s.torrents[i])
			}
		}
	} else {
		refs = make([]*Torrent, 0, len(s.torrents))
		for _, t := range s.torrents {
			if filter.matches(t) {
				refs = append(refs, t)
			}
		}
	}

	sortTorrentRefs(refs, options.Sort, options.Reverse)
	return pageTorrentRefs(refs, options.Offset, options.Limit)
}

func (s *SyncSnapshot) torrentMap() map[string]Torrent {
	m := make(map[string]Torrent, len(s.torrents))
	for _, t := range s.torrents {
		m[t.Hash] = *t
	}
	return m
}

func copyTorrents(refs []*Torrent) []Torrent {
	torrents := make([]Torrent, len(refs))
	for i, t := range refs {
		torrents[i] = *t
	}
	return torrents
}

func (s *SyncSnapshot) refs() ([]*Torrent, map[string]int) {
	if s == nil {
		return nil, nil
	}
	return s.torrents, s.index
}

// nextFull builds the snapshot after a full update. A torrent whose values
// are equal to those of s keeps its object.
func (s *SyncSnapshot) nextFull(update map[string]Torrent) (next *SyncSnapshot, removed []string) {
	prev, prevIndex := s.refs()
	torrents := make([]*Torrent, 0, len(update))
	for _, old := range prev {
		t, ok := update[old.Hash]
		if !ok {
			removed = append(removed, old.Hash)
			continue
		}
		t.Hash = old.Hash
		if torrentEqual(old, &t) {
			torrents = append(torrents, old)
		} else {
			changed := t
			torrents = append(torrents, &changed)
		}
	}

	var added []string
	for hash := range update {
		if _, ok := prevIndex[hash]; !ok {
			added = append(added, hash)
		}
	}
	slices.Sort(added)
	for _, hash := range added {
		t := update[hash]
		t.Hash = hash
		torrents = append(torrents, &t)
	}

	index := prevIndex
	if len(removed) > 0 || len(added) > 0 {
		index = indexTorrents(torrents)
	}
	return &SyncSnapshot{torrents: torrents, index: index}, removed
}

// nextPartial builds the snapshot after a partial update. updates holds the
// changed fields of each torrent, keyed by hash. A torrent that the update
// does not name keeps its object.
func (s *SyncSnapshot) nextPartial(updates map[string]any, removed []string) (next *SyncSnapshot, changed, leftOut []string) {
	torrents, index := s.refs()
	cloned := false
	var added []*Torrent
	for _, hash := range slices.Sorted(maps.Keys(updates)) {
		fields, ok := updates[hash].(map[string]any)
		if !ok {
			continue
		}
		if i, ok := index[hash]; ok {
			if !cloned {
				torrents = slices.Clone(torrents)
				cloned = true
			}
			t := *torrents[i]
			updateTorrentFields(&t, fields)
			t.Hash = hash
			torrents[i] = &t
		} else {
			if !completeNewTorrent(fields) {
				leftOut = append(leftOut, hash)
				continue
			}
			t := &Torrent{Hash: hash}
			updateTorrentFields(t, fields)
			t.Hash = hash
			added = append(added, t)
		}
		changed = append(changed, hash)
	}

	switch {
	case len(removed) > 0:
		drop := make(map[string]struct{}, len(removed))
		for _, hash := range removed {
			drop[hash] = struct{}{}
		}
		kept := make([]*Torrent, 0, len(torrents)+len(added))
		for _, t := range torrents {
			if _, ok := drop[t.Hash]; !ok {
				kept = append(kept, t)
			}
		}
		for _, t := range added {
			if _, ok := drop[t.Hash]; !ok {
				kept = append(kept, t)
			}
		}
		torrents = kept
		index = indexTorrents(torrents)
	case len(added) > 0:
		grown := make([]*Torrent, len(torrents), len(torrents)+len(added))
		copy(grown, torrents)
		index = maps.Clone(index)
		if index == nil {
			index = make(map[string]int, len(added))
		}
		for _, t := range added {
			index[t.Hash] = len(grown)
			grown = append(grown, t)
		}
		torrents = grown
	}
	return &SyncSnapshot{torrents: torrents, index: index}, changed, leftOut
}

func indexTorrents(torrents []*Torrent) map[string]int {
	index := make(map[string]int, len(torrents))
	for i, t := range torrents {
		index[t.Hash] = i
	}
	return index
}

// snapshotSeal keeps a checksum of each torrent of one snapshot, so that the
// next sync can find a torrent that a reader changed.
type snapshotSeal struct {
	seed     maphash.Seed
	snapshot *SyncSnapshot
	sums     []uint64
}

func newSnapshotSeal() *snapshotSeal {
	return &snapshotSeal{seed: maphash.MakeSeed()}
}

func (s *snapshotSeal) record(snapshot *SyncSnapshot) {
	var h maphash.Hash
	h.SetSeed(s.seed)
	sums := make([]uint64, len(snapshot.torrents))
	for i, t := range snapshot.torrents {
		h.Reset()
		writeTorrentChecksum(&h, t)
		sums[i] = h.Sum64()
	}
	s.snapshot = snapshot
	s.sums = sums
}

// verify panics with the hash of the first torrent of the recorded snapshot
// whose checksum changed.
func (s *snapshotSeal) verify() {
	if s.snapshot == nil {
		return
	}
	var h maphash.Hash
	h.SetSeed(s.seed)
	for i, t := range s.snapshot.torrents {
		h.Reset()
		writeTorrentChecksum(&h, t)
		if h.Sum64() != s.sums[i] {
			hash := t.Hash
			for key, pos := range s.snapshot.index {
				if pos == i {
					hash = key
					break
				}
			}
			panic(fmt.Sprintf("qbittorrent: torrent %s of sync snapshot %d changed after the sync manager published it", hash, s.snapshot.number))
		}
	}
}

func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func checksumUint(h *maphash.Hash, v uint64) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], v)
	_, _ = h.Write(b[:])
}

func checksumString(h *maphash.Hash, v string) {
	checksumUint(h, uint64(len(v)))
	_, _ = h.WriteString(v)
}

func checksumBool(h *maphash.Hash, v bool) {
	if v {
		_ = h.WriteByte(1)
	} else {
		_ = h.WriteByte(0)
	}
}
