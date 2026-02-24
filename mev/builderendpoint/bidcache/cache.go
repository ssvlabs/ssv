package bidcache

import (
	"sync"
	"time"

	builderspec "github.com/attestantio/go-builder-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
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

type Cache struct {
	ttl time.Duration
	now func() time.Time

	mu sync.RWMutex
	m  map[Key]Entry
}

type Option func(*Cache)

func WithNow(now func() time.Time) Option {
	return func(c *Cache) {
		c.now = now
	}
}

func New(ttl time.Duration, opts ...Option) *Cache {
	c := &Cache{
		ttl: ttl,
		now: time.Now,
		m:   make(map[Key]Entry),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
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

func (c *Cache) Put(key Key, bid *builderspec.VersionedSignedBuilderBid, provenance string) {
	var expiresAt time.Time
	if c.ttl > 0 {
		expiresAt = c.now().Add(c.ttl)
	}

	c.mu.Lock()
	c.m[key] = Entry{
		Bid:        bid,
		Provenance: provenance,
		ExpiresAt:  expiresAt,
	}
	c.mu.Unlock()
}
