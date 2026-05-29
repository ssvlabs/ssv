// Package proposersig holds the shared proposer-domain signing-root cache
// used by both the bare-OBFT and 2abOBFT runner-layer proposer signers.
//
// Both adapters wrap an inner BLS signer so that OBFT V — encoded as
// [version | SSZ blinded block] — is translated into the block's
// proposer-domain signing root before partial signing / verification. That
// translation (SSZ-unmarshal of the blinded block + hash-tree-root +
// ETH-domain compute) is identical across the two variants and is the
// dominant per-call cost; memoising it (audit F2) is the same knowledge in
// both, so it lives here once rather than as hand-aligned copies.
//
// Only the two genuinely per-package pieces are injected: the candidate
// decode functions (each adapter has its own mirror in candidate.go) and an
// error-message prefix. Everything else — the cache, the slot/root/domain
// derivation — is shared.
package proposersig

import (
	"crypto/sha256"
	"fmt"
	"sync"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
)

// DecodeCandidateFunc splits an encoded V into (version, blinded SSZ).
// Each adapter passes its package-local DecodeCandidate.
type DecodeCandidateFunc func(value []byte) (spec.DataVersion, []byte, error)

// DecodeBlindedFunc decodes a blinded SSZ block into a VersionedProposal.
// Each adapter passes its package-local DecodeBlindedProposal.
type DecodeBlindedFunc func(version spec.DataVersion, blindedSSZ []byte) (*api.VersionedProposal, error)

// Cache memoises proposer-domain signing roots keyed by sha256(V).
//
// The same V is signed/verified many times within a slot (Phase-1 retention,
// the σ-walk batch where N tuples share one V, the σ-walk fallback,
// VerifyAggregate on the cert path), so caching collapses to one real
// translation per distinct V over the cache's lifetime. Audit F2.
//
// Fork-safety: V's SSZ bytes carry the block's slot, and a given slot
// belongs to exactly one fork; distinct (slot, fork) pairs therefore produce
// distinct V bytes and distinct keys. The static sha256(V) key is fork-safe.
//
// Concurrency: a Cache is shared between the runner Instance (called under
// r.instanceMu) and the validation-layer Verifier path (on the message-
// validation pool's goroutines). The RWMutex serialises map writes while
// letting reads run concurrently.
//
// Lifetime: the Cache has no eviction — it grows by one entry per distinct V
// it observes. Memory is bounded by the owning signer's lifetime, and both
// owners are bounded: the runner-side proposer signer is per validator-share
// (a few V's per slot, GC'd when the runner retires), and the validation-layer
// proposer signer lives inside a per-validator Verifier that the message-
// validation layer TTL-evicts (see message/validation/verifier_cache.go). So
// no in-Cache eviction is needed.
type Cache struct {
	beacon          *networkconfig.Beacon
	decodeCandidate DecodeCandidateFunc
	decodeBlinded   DecodeBlindedFunc
	errPrefix       string

	mu    sync.RWMutex
	cache map[[32]byte][]byte
}

// New builds a Cache. `decodeCandidate` / `decodeBlinded` are the adapter's
// package-local candidate decoders; `errPrefix` namespaces error messages
// (e.g. "obft proposer signer" / "twoab proposer signer").
func New(
	beacon *networkconfig.Beacon,
	decodeCandidate DecodeCandidateFunc,
	decodeBlinded DecodeBlindedFunc,
	errPrefix string,
) *Cache {
	return &Cache{
		beacon:          beacon,
		decodeCandidate: decodeCandidate,
		decodeBlinded:   decodeBlinded,
		errPrefix:       errPrefix,
		cache:           make(map[[32]byte][]byte),
	}
}

// SigningRoot returns the proposer-domain signing root for `value`, memoised
// by sha256(value). Misses fall through to the uncached SSZ + tree-root +
// domain compute; nothing is cached on error.
func (c *Cache) SigningRoot(value []byte) ([]byte, error) {
	if len(value) == 0 {
		// Empty value falls through to the decoder, which returns a
		// structured error. Don't poison the cache with the all-zero key.
		return c.signingRootUncached(value)
	}
	key := sha256.Sum256(value)

	c.mu.RLock()
	sr, ok := c.cache[key]
	c.mu.RUnlock()
	if ok {
		return sr, nil
	}

	sr, err := c.signingRootUncached(value)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	// Re-check under the write lock — a concurrent miss may have already
	// populated. Keep whichever was stored first (the values are equal).
	if existing, ok := c.cache[key]; ok {
		c.mu.Unlock()
		return existing, nil
	}
	c.cache[key] = sr
	c.mu.Unlock()
	return sr, nil
}

func (c *Cache) signingRootUncached(value []byte) ([]byte, error) {
	version, blindedSSZ, err := c.decodeCandidate(value)
	if err != nil {
		return nil, err
	}
	vBlk, err := c.decodeBlinded(version, blindedSSZ)
	if err != nil {
		return nil, err
	}
	slot, err := vBlk.Slot()
	if err != nil {
		return nil, fmt.Errorf("%s: extract slot: %w", c.errPrefix, err)
	}
	root, err := vBlk.Root()
	if err != nil {
		return nil, fmt.Errorf("%s: compute block root: %w", c.errPrefix, err)
	}
	epoch := c.beacon.EstimatedEpochAtSlot(slot)
	_, fork := c.beacon.ForkAtEpoch(epoch)
	domain, err := spectypes.ComputeETHDomain(spectypes.DomainProposer, fork.CurrentVersion, c.beacon.GenesisValidatorsRoot)
	if err != nil {
		return nil, fmt.Errorf("%s: compute domain: %w", c.errPrefix, err)
	}
	signingContainer := phase0.SigningData{ObjectRoot: root, Domain: domain}
	sr, err := signingContainer.HashTreeRoot()
	if err != nil {
		return nil, fmt.Errorf("%s: hash signing data: %w", c.errPrefix, err)
	}
	return sr[:], nil
}

// Len reports the number of distinct V's currently memoised. Intended for
// tests / introspection; safe to call concurrently.
func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cache)
}
