package obft

import (
	"errors"

	"github.com/ssvlabs/ssv/networkconfig"
	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft/base"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner/obft/proposersig"
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
//
// The V → signing-root translation is memoised by the shared proposersig.Cache
// (audit F2) — see that package for the cache's fork-safety / concurrency /
// lifetime rationale. It is identical knowledge in the 2abOBFT sibling, so it
// lives there once rather than as hand-aligned copies.
type proposerSigner struct {
	inner obftcore.Signer
	sr    *proposersig.Cache
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
		inner: inner,
		sr:    proposersig.New(beacon, DecodeCandidate, DecodeBlindedProposal, "obft proposer signer"),
	}, nil
}

// signingRootFor returns the proposer-domain signing root for `value`,
// delegating to the shared, memoised proposersig.Cache (audit F2).
func (s *proposerSigner) signingRootFor(value []byte) ([]byte, error) {
	return s.sr.SigningRoot(value)
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
// per-layer "many ops sign one V" pattern). The shared proposersig.Cache
// (F2) collapses the per-msg signingRootFor calls to one real translation
// per distinct V, so this loop is effectively one translate + N-1 sub-µs
// map lookups — no longer redundant work.
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
