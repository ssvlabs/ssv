package twoab

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
	twoabcore "github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
)

// proposerSigner wraps an inner BLS signer such that 2abOBFT V — encoded as
// [version | SSZ blinded block] — is translated into the block's
// proposer-domain signing root before partial signing / verification. The
// resulting aggregated signature is therefore directly usable as a
// beacon-block proposer signature.
//
// Mirror of the bare-OBFT proposerSigner (protocol/v2/ssv/runner/obft).
// Use only for the V-side share; the IBE TagSigner must remain the inner raw
// signer because NR partials sign opaque nr_tag bytes, not SSV blocks.
type proposerSigner struct {
	inner  twoabcore.Signer
	beacon *networkconfig.Beacon

	// srMu / srCache memoise signingRootFor results keyed by sha256(V).
	// Mirror of the bare-OBFT proposerSigner's srCache — see that one's
	// field doc (protocol/v2/ssv/runner/obft/proposer_signer.go) for the
	// fork-safety / concurrency / lifetime argument. Audit F2.
	srMu    sync.RWMutex
	srCache map[[32]byte][]byte
}

// NewProposerSigner wraps `inner` so partial sigs are computed over the
// proposer-domain signing root of the block carried in V. `beacon` provides
// fork schedule + GenesisValidatorsRoot for domain derivation.
func NewProposerSigner(inner twoabcore.Signer, beacon *networkconfig.Beacon) (twoabcore.Signer, error) {
	if inner == nil {
		return nil, errors.New("twoab proposer signer: nil inner signer")
	}
	if beacon == nil {
		return nil, errors.New("twoab proposer signer: nil beacon config")
	}
	return &proposerSigner{
		inner:   inner,
		beacon:  beacon,
		srCache: make(map[[32]byte][]byte),
	}, nil
}

// signingRootFor returns the proposer-domain signing root for `value`,
// memoised in srCache. See the base proposerSigner's signingRootFor doc
// for the F2 design notes; this is a 1:1 mirror.
func (s *proposerSigner) signingRootFor(value []byte) ([]byte, error) {
	if len(value) == 0 {
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
		return nil, fmt.Errorf("twoab proposer signer: extract slot: %w", err)
	}
	root, err := vBlk.Root()
	if err != nil {
		return nil, fmt.Errorf("twoab proposer signer: compute block root: %w", err)
	}
	epoch := s.beacon.EstimatedEpochAtSlot(slot)
	_, fork := s.beacon.ForkAtEpoch(epoch)
	domain, err := spectypes.ComputeETHDomain(spectypes.DomainProposer, fork.CurrentVersion, s.beacon.GenesisValidatorsRoot)
	if err != nil {
		return nil, fmt.Errorf("twoab proposer signer: compute domain: %w", err)
	}
	signingContainer := phase0.SigningData{ObjectRoot: root, Domain: domain}
	sr, err := signingContainer.HashTreeRoot()
	if err != nil {
		return nil, fmt.Errorf("twoab proposer signer: hash signing data: %w", err)
	}
	return sr[:], nil
}

func (s *proposerSigner) SignPartial(msg []byte) (twoabcore.Signature, error) {
	sr, err := s.signingRootFor(msg)
	if err != nil {
		return nil, err
	}
	return s.inner.SignPartial(sr)
}

func (s *proposerSigner) VerifyPartial(pubKeyShare []byte, msg []byte, partial twoabcore.Signature) bool {
	sr, err := s.signingRootFor(msg)
	if err != nil {
		return false
	}
	return s.inner.VerifyPartial(pubKeyShare, sr, partial)
}

func (s *proposerSigner) AggregatePartials(partials map[twoabcore.OperatorID]twoabcore.Signature) (twoabcore.Signature, error) {
	return s.inner.AggregatePartials(partials)
}

func (s *proposerSigner) VerifyAggregate(clusterPubKey []byte, msg []byte, sig twoabcore.Signature) bool {
	sr, err := s.signingRootFor(msg)
	if err != nil {
		return false
	}
	return s.inner.VerifyAggregate(clusterPubKey, sr, sig)
}

// VerifyPartialBatch translates each msg (V bytes) to its proposer-domain
// signing root then delegates the batch to the inner signer. Mirror of the
// base-OBFT proposerSigner — F2's srCache collapses the per-msg translations
// to one real signing-root compute per distinct V, so the σ-walk's "same V
// across N tuples" pattern pays one ~100 µs translate + N-1 sub-µs lookups
// rather than N × ~100 µs.
func (s *proposerSigner) VerifyPartialBatch(pubKeyShares [][]byte, msgs [][]byte, sigs []twoabcore.Signature) bool {
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
