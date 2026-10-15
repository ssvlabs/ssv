package versionprobe

import (
	"sort"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// Entry is the probe history of one peer.
type Entry struct {
	PeerID      peer.ID
	Version     string    // last successfully probed version; kept across later failures
	ProbedAt    time.Time // time of the last success
	LastAttempt time.Time
	Outcome     Outcome // outcome of the last attempt
	Error       string
	Attempts    int
	failures    int // consecutive failures since the last success
	NextAttempt time.Time
	AddrSource  string   // where the last attempt's addresses came from ("" when none were found)
	Addrs       []string // addresses dialed in the last attempt, at most maxRecordedAddrs
}

// Cache holds probe results per peer, bounded to max entries.
type Cache struct {
	mu      sync.Mutex
	max     int
	entries map[peer.ID]*Entry
}

// NewCache returns an empty cache holding at most maxPeers peers.
func NewCache(maxPeers int) *Cache {
	return &Cache{max: maxPeers, entries: make(map[peer.ID]*Entry)}
}

// Due reports whether the peer may be probed at now.
func (c *Cache) Due(id peer.ID, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[id]
	return !ok || !now.Before(e.NextAttempt)
}

// Record stores the result of an attempt made at now and returns the updated entry.
func (c *Cache) Record(id peer.ID, r Result, now time.Time) Entry {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[id]
	if !ok {
		if len(c.entries) >= c.max {
			c.evictOldestLocked()
		}
		e = &Entry{PeerID: id}
		c.entries[id] = e
	}
	e.Attempts++
	e.LastAttempt = now
	e.Outcome = r.Outcome
	e.Error = r.Err
	e.AddrSource = r.AddrSource
	e.Addrs = nil
	if len(r.Addrs) > 0 {
		e.Addrs = append([]string(nil), r.Addrs[:min(len(r.Addrs), maxRecordedAddrs)]...)
	}
	if r.Outcome == OutcomeOK {
		e.Version = r.Version
		e.ProbedAt = now
		e.failures = 0
		e.NextAttempt = now.Add(refreshAfter)
	} else {
		e.failures++
		i := min(e.failures, len(backoffSchedule)) - 1
		e.NextAttempt = now.Add(backoffSchedule[i])
	}
	return *e
}

// Version returns the last successfully probed version of a peer.
func (c *Cache) Version(id peer.ID) (string, time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[id]
	if !ok || e.Version == "" {
		return "", time.Time{}, false
	}
	return e.Version, e.ProbedAt, true
}

// Entries returns a copy of all entries, most recent attempt first.
func (c *Cache) Entries() []Entry {
	c.mu.Lock()
	out := make([]Entry, 0, len(c.entries))
	for _, e := range c.entries {
		out = append(out, *e)
	}
	c.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if !out[i].LastAttempt.Equal(out[j].LastAttempt) {
			return out[i].LastAttempt.After(out[j].LastAttempt)
		}
		return out[i].PeerID < out[j].PeerID
	})
	return out
}

func (c *Cache) evictOldestLocked() {
	var oldest *Entry
	for _, e := range c.entries {
		if oldest == nil || e.LastAttempt.Before(oldest.LastAttempt) {
			oldest = e
		}
	}
	if oldest != nil {
		delete(c.entries, oldest.PeerID)
	}
}
