package obft

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft/base"
)

// proposerSigner wraps an inner BLS signer such that OBFT V — encoded as
// [version | SSZ blinded block] — is translated into the block's
// proposer-domain signing root before partial signing / verification. The
// resulting OBFT-aggregated signature is therefore directly usable as a
// beacon-block proposer signature (verified against the block's
// `compute_signing_root(block, Domain_Proposer)`).
//
// Without this wrapper, OBFT partials would sign the raw block bytes, and
// the aggregated signature would not be accepted by the beacon node.
//
// Use only for the V-side share. The IBE TagSigner must remain the inner
// raw signer because NR partials sign opaque nr_tag bytes, not SSV blocks.
type proposerSigner struct {
	inner  obftcore.Signer
	beacon *networkconfig.Beacon

	// srMu / srCache memoise signingRootFor results, keyed by sha256(V).
	// The translation (SSZ-unmarshal of the blinded block + hash-tree-root
	// + ETH-domain compute) costs ~100 µs and allocs ~336 / ~22 KB per
	// call on a 17 KB block (B2). Audit F2: the same V is signed/verified
	// repeatedly within a slot — leader-bundle path, σ-walk batch path
	// (where N tuples share one V), σ-walk fallback path — so caching
	// collapses to one translation per distinct V over the signer's
	// lifetime.
	//
	// Fork-safety: V's SSZ bytes carry the block's slot, and a given slot
	// belongs to exactly one fork; distinct (slot, fork) pairs therefore
	// produce distinct V bytes and distinct cache keys. A static
	// sha256(V) key is fork-safe in practice — V from fork F1 can never
	// collide with V from F2 because the slot field disambiguates.
	//
	// Concurrency: this proposerSigner is shared between the runner's
	// Instance (called under r.instanceMu — single-threaded per slot) and
	// the validation-layer Verifier path (per-envelope, on the message-
	// validation pool's goroutines). The RWMutex serialises map writes
	// while letting reads run concurrently.
	//
	// Lifetime / unbounded growth: the runner-side proposerSigner lives
	// per validator-share, not per slot — it survives across slots. The
	// cache therefore grows by 1-3 entries per slot (own V plus possible
	// peer equivocations). At ~64 bytes per entry (32-byte key + 32-byte
	// signing-root), this is unbounded but tiny in practice (1 KB per
	// ~16 slots; ~12 KB per epoch); GC reclaims it when the runner is
	// retired. The validation-layer wrapper is per-envelope, so its cache
	// is single-use and effectively zero-size; F2 helps the validation
	// path only marginally.
	srMu    sync.RWMutex
	srCache map[[32]byte][]byte
}

// NewProposerSigner wraps `inner` so partial sigs are computed over the
// proposer-domain signing root of the block carried in V. `beacon` provides
// fork schedule + GenesisValidatorsRoot for domain derivation.
func NewProposerSigner(inner obftcore.Signer, beacon *networkconfig.Beacon) (obftcore.Signer, error) {
	if inner == nil {
		return nil, errors.New("obft proposer signer: nil inner signer")
	}
	if beacon == nil {
		return nil, errors.New("obft proposer signer: nil beacon config")
	}
	return &proposerSigner{
		inner:   inner,
		beacon:  beacon,
		srCache: make(map[[32]byte][]byte),
	}, nil
}

// signingRootFor returns the proposer-domain signing root for `value`,
// memoised in srCache keyed by sha256(value). Cache misses fall through
// to signingRootForUncached which does the SSZ + tree-root + domain
// compute. On error nothing is cached.
//
// Audit F2 (docs/OBFT-PERFORMANCE-AUDIT-PLAN.md §F2). See the srCache
// field doc for the safety / lifetime / fork-safety argument.
func (s *proposerSigner) signingRootFor(value []byte) ([]byte, error) {
	if len(value) == 0 {
		// Mirror the previous behaviour: an empty value falls through to
		// DecodeCandidate which returns a structured error. Don't poison
		// the cache with the empty-key entry.
		return s.signingRootForUncached(value)
	}
	key := sha256.Sum256(value)
	s.srMu.RLock()
	sr, ok := s.srCache[key]
	s.srMu.RUnlock()
	if ok {
		return sr, nil
	}
	sr, err := s.signingRootForUncached(value)
	if err != nil {
		return nil, err
	}
	s.srMu.Lock()
	// Re-check under the write lock — a concurrent miss may have already
	// populated. Keep whichever was stored first (the values are
	// mathematically equal; either entry is correct).
	if existing, ok := s.srCache[key]; ok {
		s.srMu.Unlock()
		return existing, nil
	}
	s.srCache[key] = sr
	s.srMu.Unlock()
	return sr, nil
}

func (s *proposerSigner) signingRootForUncached(value []byte) ([]byte, error) {
	version, blindedSSZ, err := DecodeCandidate(value)
	if err != nil {
		return nil, err
	}
	vBlk, err := DecodeBlindedProposal(version, blindedSSZ)
	if err != nil {
		return nil, err
	}
	slot, err := vBlk.Slot()
	if err != nil {
		return nil, fmt.Errorf("obft proposer signer: extract slot: %w", err)
	}
	root, err := vBlk.Root()
	if err != nil {
		return nil, fmt.Errorf("obft proposer signer: compute block root: %w", err)
	}
	epoch := s.beacon.EstimatedEpochAtSlot(slot)
	_, fork := s.beacon.ForkAtEpoch(epoch)
	domain, err := spectypes.ComputeETHDomain(spectypes.DomainProposer, fork.CurrentVersion, s.beacon.GenesisValidatorsRoot)
	if err != nil {
		return nil, fmt.Errorf("obft proposer signer: compute domain: %w", err)
	}
	signingContainer := phase0.SigningData{ObjectRoot: root, Domain: domain}
	sr, err := signingContainer.HashTreeRoot()
	if err != nil {
		return nil, fmt.Errorf("obft proposer signer: hash signing data: %w", err)
	}
	return sr[:], nil
}

func (s *proposerSigner) SignPartial(msg []byte) (obftcore.Signature, error) {
	sr, err := s.signingRootFor(msg)
	if err != nil {
		return nil, err
	}
	return s.inner.SignPartial(sr)
}

func (s *proposerSigner) VerifyPartial(pubKeyShare []byte, msg []byte, partial obftcore.Signature) bool {
	sr, err := s.signingRootFor(msg)
	if err != nil {
		return false
	}
	return s.inner.VerifyPartial(pubKeyShare, sr, partial)
}

func (s *proposerSigner) AggregatePartials(partials map[obftcore.OperatorID]obftcore.Signature) (obftcore.Signature, error) {
	return s.inner.AggregatePartials(partials)
}

func (s *proposerSigner) VerifyAggregate(clusterPubKey []byte, msg []byte, sig obftcore.Signature) bool {
	sr, err := s.signingRootFor(msg)
	if err != nil {
		return false
	}
	return s.inner.VerifyAggregate(clusterPubKey, sr, sig)
}

// VerifyPartialBatch translates each msg (V bytes) to its proposer-domain
// signing root then delegates the batch to the inner signer. Each msg is
// translated independently — if any translation fails the whole batch
// returns false, matching the per-tuple short-circuit semantics of the
// inner backends.
//
// In F4's σ-walk caller, every tuple in a batch shares the same V (the
// per-layer "many ops sign one V" pattern). F2's srCache collapses the
// per-msg signingRootFor calls to one real translation per distinct V,
// so this loop is effectively `signingRootForUncached` once + N-1
// sub-µs map lookups — no longer redundant work.
func (s *proposerSigner) VerifyPartialBatch(pubKeyShares [][]byte, msgs [][]byte, sigs []obftcore.Signature) bool {
	n := len(sigs)
	if n == 0 || len(pubKeyShares) != n || len(msgs) != n {
		return false
	}
	srs := make([][]byte, n)
	for i, m := range msgs {
		sr, err := s.signingRootFor(m)
		if err != nil {
			return false
		}
		srs[i] = sr
	}
	return s.inner.VerifyPartialBatch(pubKeyShares, srs, sigs)
}
