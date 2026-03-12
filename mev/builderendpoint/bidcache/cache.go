package bidcache

import (
	"sync"
	"time"

	builderspec "github.com/attestantio/go-builder-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"

	prov "github.com/ssvlabs/ssv/mev/builderendpoint/provenance"
)

type Key struct {
	Slot       phase0.Slot
	ParentHash phase0.Hash32
	Pubkey     phase0.BLSPubKey
}

type Entry struct {
	Bid        *builderspec.VersionedSignedBuilderBid
	Provenance string
	ExpiresAt  time.Time
}

type provenanceEntry struct {
	Provenance string
	ExpiresAt  time.Time
}

type Cache struct {
	ttl time.Duration
	now func() time.Time

	mu sync.RWMutex
	m  map[Key]Entry

	// byExecBlockHash stores the relay provenance keyed by (slot, execution payload block hash).
	// This is used for provenance-based unblind routing.
	byExecBlockHash map[prov.Key]provenanceEntry
}

func New(ttl time.Duration) *Cache {
	c := &Cache{
		ttl: ttl,
		now: time.Now,
		m:   make(map[Key]Entry),

		byExecBlockHash: make(map[prov.Key]provenanceEntry),
	}
	return c
}

func (c *Cache) Get(key Key) (Entry, bool) {
	c.mu.RLock()
	ent, ok := c.m[key]
	c.mu.RUnlock()

	if !ok {
		return Entry{}, false
	}
	if !ent.ExpiresAt.IsZero() && c.now().After(ent.ExpiresAt) {
		c.mu.Lock()
		// Re-check under write lock to avoid races.
		ent2, ok2 := c.m[key]
		if ok2 && ent2.ExpiresAt.Equal(ent.ExpiresAt) {
			delete(c.m, key)
		}
		c.mu.Unlock()
		return Entry{}, false
	}
	return ent, true
}

func (c *Cache) Put(key Key, bid *builderspec.VersionedSignedBuilderBid, relayProvenance string) {
	var expiresAt time.Time
	if c.ttl > 0 {
		expiresAt = c.now().Add(c.ttl)
	}

	c.mu.Lock()
	c.m[key] = Entry{
		Bid:        bid,
		Provenance: relayProvenance,
		ExpiresAt:  expiresAt,
	}

	if relayProvenance != "" {
		if provKey, ok := prov.FromBid(key.Slot, bid); ok {
			c.byExecBlockHash[provKey] = provenanceEntry{
				Provenance: relayProvenance,
				ExpiresAt:  expiresAt,
			}
		}
	}
	c.mu.Unlock()
}

// PutIfBetter stores bid in the cache if:
// - this is the first entry for the key, or
// - bid's value is greater than the existing entry's value, or
// - the existing value cannot be evaluated.
//
// It returns (first, updated).
// - first is true if there was no existing (non-expired) entry for the key.
// - updated is true if the cache was written (including first insert).
func (c *Cache) PutIfBetter(key Key, bid *builderspec.VersionedSignedBuilderBid, relayProvenance string) (first bool, updated bool) {
	if c == nil {
		return false, false
	}

	var expiresAt time.Time
	if c.ttl > 0 {
		expiresAt = c.now().Add(c.ttl)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Check existing entry, treating expired entries as absent.
	ent, ok := c.m[key]
	if ok && !ent.ExpiresAt.IsZero() && c.now().After(ent.ExpiresAt) {
		delete(c.m, key)
		ok = false
	}
	if !ok {
		c.m[key] = Entry{
			Bid:        bid,
			Provenance: relayProvenance,
			ExpiresAt:  expiresAt,
		}
		if relayProvenance != "" {
			if provKey, ok := prov.FromBid(key.Slot, bid); ok {
				c.byExecBlockHash[provKey] = provenanceEntry{
					Provenance: relayProvenance,
					ExpiresAt:  expiresAt,
				}
			}
		}
		return true, true
	}

	if ent.Bid == nil || ent.Bid.IsEmpty() {
		// Existing entry is unusable; overwrite.
		c.m[key] = Entry{
			Bid:        bid,
			Provenance: relayProvenance,
			ExpiresAt:  expiresAt,
		}
		if relayProvenance != "" {
			if provKey, ok := prov.FromBid(key.Slot, bid); ok {
				c.byExecBlockHash[provKey] = provenanceEntry{
					Provenance: relayProvenance,
					ExpiresAt:  expiresAt,
				}
			}
		}
		return false, true
	}

	if bid == nil || bid.IsEmpty() {
		// Never overwrite with an empty bid.
		return false, false
	}

	newValue, errNew := bid.Value()
	if errNew != nil || newValue == nil {
		// Cannot compare; keep existing.
		return false, false
	}

	oldValue, errOld := ent.Bid.Value()
	shouldOverwrite := errOld != nil || oldValue == nil || newValue.Cmp(oldValue) > 0
	if !shouldOverwrite {
		return false, false
	}

	// Remove old provenance mapping to avoid stale routing entries.
	if ent.Provenance != "" {
		if oldProvKey, ok := prov.FromBid(key.Slot, ent.Bid); ok {
			delete(c.byExecBlockHash, oldProvKey)
		}
	}

	c.m[key] = Entry{
		Bid:        bid,
		Provenance: relayProvenance,
		ExpiresAt:  expiresAt,
	}
	if relayProvenance != "" {
		if provKey, ok := prov.FromBid(key.Slot, bid); ok {
			c.byExecBlockHash[provKey] = provenanceEntry{
				Provenance: relayProvenance,
				ExpiresAt:  expiresAt,
			}
		}
	}
	return false, true
}

func (c *Cache) GetProvenanceByBlockHash(slot phase0.Slot, blockHash phase0.Hash32) (string, bool) {
	if c == nil {
		return "", false
	}
	key := prov.Key{Slot: slot, BlockHash: blockHash}

	c.mu.RLock()
	ent, ok := c.byExecBlockHash[key]
	c.mu.RUnlock()

	if !ok {
		return "", false
	}
	if !ent.ExpiresAt.IsZero() && c.now().After(ent.ExpiresAt) {
		c.mu.Lock()
		// Re-check under write lock to avoid races.
		ent2, ok2 := c.byExecBlockHash[key]
		if ok2 && ent2.ExpiresAt.Equal(ent.ExpiresAt) {
			delete(c.byExecBlockHash, key)
		}
		c.mu.Unlock()
		return "", false
	}
	return ent.Provenance, true
}

// CleanupExpired proactively removes expired entries from the cache.
//
// Expired entries are also removed lazily when accessed via Get(), but prefetching may insert keys
// that are never requested again. Periodic cleanup prevents unbounded growth of expired entries.
func (c *Cache) CleanupExpired() {
	if c == nil {
		return
	}

	now := c.now()

	c.mu.Lock()
	for k, ent := range c.m {
		if !ent.ExpiresAt.IsZero() && now.After(ent.ExpiresAt) {
			delete(c.m, k)
		}
	}
	for k, ent := range c.byExecBlockHash {
		if !ent.ExpiresAt.IsZero() && now.After(ent.ExpiresAt) {
			delete(c.byExecBlockHash, k)
		}
	}
	c.mu.Unlock()
}

// Sizes returns the number of bid entries and provenance entries currently stored.
func (c *Cache) Sizes() (bidEntries int, provenanceEntries int) {
	if c == nil {
		return 0, 0
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.m), len(c.byExecBlockHash)
}
