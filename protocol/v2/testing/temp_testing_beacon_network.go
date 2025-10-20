package testing

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

type BeaconNodeWrapped struct {
	beacon.BeaconNode
	Bn *spectestingutils.TestingBeaconNode
}

func (bn *BeaconNodeWrapped) SetSyncCommitteeAggregatorRootHexes(roots map[string]bool) {
	bn.Bn.SetSyncCommitteeAggregatorRootHexes(roots)
}

func (bn *BeaconNodeWrapped) GetBroadcastedRoots() []phase0.Root {
	return bn.Bn.BroadcastedRoots
}

func (bn *BeaconNodeWrapped) GetBeaconNode() *spectestingutils.TestingBeaconNode {
	return bn.Bn
}

func (bn *BeaconNodeWrapped) GetAttestationData(ctx context.Context, slot phase0.Slot) (*phase0.AttestationData, spec.DataVersion, error) {
	return bn.Bn.GetAttestationData(slot)
}
func (bn *BeaconNodeWrapped) DomainData(ctx context.Context, epoch phase0.Epoch, domain phase0.DomainType) (phase0.Domain, error) {
	return bn.Bn.DomainData(epoch, domain)
}
func (bn *BeaconNodeWrapped) SyncCommitteeSubnetID(index phase0.CommitteeIndex) uint64 {
	return bn.Bn.SyncCommitteeSubnetID(index)
}
func (bn *BeaconNodeWrapped) IsSyncCommitteeAggregator(proof []byte) bool {
	return bn.Bn.IsSyncCommitteeAggregator(proof)
}
func (bn *BeaconNodeWrapped) GetSyncCommitteeContribution(ctx context.Context, slot phase0.Slot, selectionProofs []phase0.BLSSignature, subnetIDs []uint64) (ssz.Marshaler, spec.DataVersion, error) {
	return bn.Bn.GetSyncCommitteeContribution(slot, selectionProofs, subnetIDs)
}
func (bn *BeaconNodeWrapped) SubmitAggregateSelectionProof(ctx context.Context, slot phase0.Slot, committeeIndex phase0.CommitteeIndex, committeeLength uint64, index phase0.ValidatorIndex, slotSig []byte) (ssz.Marshaler, spec.DataVersion, error) {
	return bn.Bn.SubmitAggregateSelectionProof(slot, committeeIndex, committeeLength, index, slotSig)
}
func (bn *BeaconNodeWrapped) GetBeaconNetwork() spectypes.BeaconNetwork {
	return bn.Bn.GetBeaconNetwork()
}
func (bn *BeaconNodeWrapped) GetBeaconBlock(ctx context.Context, slot phase0.Slot, graffiti, randao []byte) (ssz.Marshaler, spec.DataVersion, error) {
	return bn.Bn.GetBeaconBlock(slot, graffiti, randao)
}
func (bn *BeaconNodeWrapped) SubmitValidatorRegistrations(ctx context.Context, registrations []*api.VersionedSignedValidatorRegistration) error {
	for _, registration := range registrations {
		err := bn.Bn.SubmitValidatorRegistration(registration)
		if err != nil {
			return err
		}
	}
	return nil
}
func (bn *BeaconNodeWrapped) SubmitVoluntaryExit(ctx context.Context, voluntaryExit *phase0.SignedVoluntaryExit) error {
	return bn.Bn.SubmitVoluntaryExit(voluntaryExit)
}
func (bn *BeaconNodeWrapped) SubmitAttestations(ctx context.Context, attestations []*spec.VersionedAttestation) error {
	return bn.Bn.SubmitAttestations(attestations)
}
func (bn *BeaconNodeWrapped) SubmitSyncMessages(ctx context.Context, msgs []*altair.SyncCommitteeMessage) error {
	return bn.Bn.SubmitSyncMessages(msgs)
}
func (bn *BeaconNodeWrapped) SubmitBlindedBeaconBlock(ctx context.Context, block *api.VersionedBlindedProposal, sig phase0.BLSSignature) error {
	return bn.Bn.SubmitBlindedBeaconBlock(block, sig)
}
func (bn *BeaconNodeWrapped) SubmitSignedContributionAndProof(ctx context.Context, contribution *altair.SignedContributionAndProof) error {
	return bn.Bn.SubmitSignedContributionAndProof(contribution)
}
func (bn *BeaconNodeWrapped) SubmitSignedAggregateSelectionProof(ctx context.Context, msg *spec.VersionedSignedAggregateAndProof) error {
	return bn.Bn.SubmitSignedAggregateSelectionProof(msg)
}
func (bn *BeaconNodeWrapped) SubmitBeaconBlock(ctx context.Context, block *api.VersionedProposal, sig phase0.BLSSignature) error {
	return bn.Bn.SubmitBeaconBlock(block, sig)
}

func NewTestingBeaconNodeWrapped() beacon.BeaconNode {
	bnw := &BeaconNodeWrapped{}
	bnw.Bn = spectestingutils.NewTestingBeaconNode()

	return bnw
}
