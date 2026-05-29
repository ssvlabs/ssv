package twoab

import (
	"errors"

	"github.com/ssvlabs/ssv/networkconfig"
	twoabcore "github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner/obft/proposersig"
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
//
// The V → signing-root translation + its F2 memoisation live in the shared
// proposersig.Cache (one copy across both variants); only the package-local
// candidate decoders + error prefix are injected.
type proposerSigner struct {
	inner twoabcore.Signer
	sr    *proposersig.Cache
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
		inner: inner,
		sr:    proposersig.New(beacon, DecodeCandidate, DecodeBlindedProposal, "twoab proposer signer"),
	}, nil
}

// signingRootFor returns the proposer-domain signing root for `value`,
// delegating to the shared, memoised proposersig.Cache (audit F2).
func (s *proposerSigner) signingRootFor(value []byte) ([]byte, error) {
	return s.sr.SigningRoot(value)
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
// base-OBFT proposerSigner — the shared proposersig.Cache (F2) collapses the
// per-msg translations to one real signing-root compute per distinct V, so
// the σ-walk's "same V across N tuples" pattern pays one translate + N-1
// sub-µs lookups rather than N × ~100 µs.
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
