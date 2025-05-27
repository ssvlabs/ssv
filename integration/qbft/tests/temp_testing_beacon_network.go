package tests

import (
	"context"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

type TestingBeaconNodeWrapped struct {
	beacon.BeaconNode
	Bn *spectestingutils.TestingBeaconNode
}

func (bn *TestingBeaconNodeWrapped) SetSyncCommitteeAggregatorRootHexes(roots map[string]bool) {
	bn.Bn.SetSyncCommitteeAggregatorRootHexes(roots)
}

func (bn *TestingBeaconNodeWrapped) GetBroadcastedRoots() []phase0.Root {
	return bn.Bn.BroadcastedRoots
}

func (bn *TestingBeaconNodeWrapped) GetBeaconNode() *spectestingutils.TestingBeaconNode {
	return bn.Bn
}

func (bn *TestingBeaconNodeWrapped) GetAttestationData(ctx context.Context, slot phase0.Slot) (*phase0.AttestationData, spec.DataVersion, error) {
	return bn.Bn.GetAttestationData(slot)
}
func (bn *TestingBeaconNodeWrapped) DomainData(ctx context.Context, epoch phase0.Epoch, domain phase0.DomainType) (phase0.Domain, error) {
	return bn.Bn.DomainData(epoch, domain)
}
func (bn *TestingBeaconNodeWrapped) SyncCommitteeSubnetID(index phase0.CommitteeIndex) uint64 {
	v, err := bn.Bn.SyncCommitteeSubnetID(index)
	if err != nil {
		panic("unexpected error from SyncCommitteeSubnetID")
	}
	return v
}
func (bn *TestingBeaconNodeWrapped) IsSyncCommitteeAggregator(proof []byte) bool {
	v, err := bn.Bn.IsSyncCommitteeAggregator(proof)
	if err != nil {
		panic("unexpected error from IsSyncCommitteeAggregator")
	}
	return v
}
func (bn *TestingBeaconNodeWrapped) GetSyncCommitteeContribution(ctx context.Context, slot phase0.Slot, selectionProofs []phase0.BLSSignature, subnetIDs []uint64) (ssz.Marshaler, spec.DataVersion, error) {
	return bn.Bn.GetSyncCommitteeContribution(slot, selectionProofs, subnetIDs)
}
func (bn *TestingBeaconNodeWrapped) SubmitAggregateSelectionProof(ctx context.Context, slot phase0.Slot, committeeIndex phase0.CommitteeIndex, committeeLength uint64, index phase0.ValidatorIndex, slotSig []byte) (ssz.Marshaler, spec.DataVersion, error) {
	return bn.Bn.SubmitAggregateSelectionProof(slot, committeeIndex, committeeLength, index, slotSig)
}
func (bn *TestingBeaconNodeWrapped) GetBeaconNetwork() spectypes.BeaconNetwork {
	return bn.Bn.GetBeaconNetwork()
}
func (bn *TestingBeaconNodeWrapped) GetBeaconBlock(ctx context.Context, slot phase0.Slot, graffiti, randao []byte) (ssz.Marshaler, spec.DataVersion, error) {
	return bn.Bn.GetBeaconBlock(slot, graffiti, randao)
}
func (bn *TestingBeaconNodeWrapped) SubmitValidatorRegistration(registration *api.VersionedSignedValidatorRegistration) error {
	return bn.Bn.SubmitValidatorRegistration(registration)
}
func (bn *TestingBeaconNodeWrapped) SubmitVoluntaryExit(ctx context.Context, voluntaryExit *phase0.SignedVoluntaryExit) error {
	return bn.Bn.SubmitVoluntaryExit(voluntaryExit)
}
func (bn *TestingBeaconNodeWrapped) SubmitAttestations(ctx context.Context, attestations []*spec.VersionedAttestation) error {
	return bn.Bn.SubmitAttestations(attestations)
}
func (bn *TestingBeaconNodeWrapped) SubmitSyncMessages(ctx context.Context, msgs []*altair.SyncCommitteeMessage) error {
	return bn.Bn.SubmitSyncMessages(msgs)
}
func (bn *TestingBeaconNodeWrapped) SubmitBlindedBeaconBlock(ctx context.Context, block *api.VersionedBlindedProposal, sig phase0.BLSSignature) error {
	return bn.Bn.SubmitBlindedBeaconBlock(block, sig)
}
func (bn *TestingBeaconNodeWrapped) SubmitSignedContributionAndProof(ctx context.Context, contribution *altair.SignedContributionAndProof) error {
	return bn.Bn.SubmitSignedContributionAndProof(contribution)
}
func (bn *TestingBeaconNodeWrapped) SubmitSignedAggregateSelectionProof(ctx context.Context, msg *spec.VersionedSignedAggregateAndProof) error {
	return bn.Bn.SubmitSignedAggregateSelectionProof(msg)
}
func (bn *TestingBeaconNodeWrapped) SubmitBeaconBlock(ctx context.Context, block *api.VersionedProposal, sig phase0.BLSSignature) error {
	return bn.Bn.SubmitBeaconBlock(block, sig)
}

func NewTestingBeaconNodeWrapped() beacon.BeaconNode {
	bnw := &TestingBeaconNodeWrapped{}
	bnw.Bn = spectestingutils.NewTestingBeaconNode()

	return bnw
}
