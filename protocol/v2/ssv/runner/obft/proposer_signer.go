package obft

import (
	"errors"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft"
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
	return &proposerSigner{inner: inner, beacon: beacon}, nil
}

func (s *proposerSigner) signingRootFor(value []byte) ([]byte, error) {
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
