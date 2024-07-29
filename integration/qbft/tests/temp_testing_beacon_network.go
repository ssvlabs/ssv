package tests

import (
	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
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

func (bn *TestingBeaconNodeWrapped) GetAttestationData(slot phase0.Slot, committeeIndex phase0.CommitteeIndex) (*phase0.AttestationData, spec.DataVersion, error) {
	return bn.Bn.GetAttestationData(slot, committeeIndex)
}
func (bn *TestingBeaconNodeWrapped) DomainData(epoch phase0.Epoch, domain phase0.DomainType) (phase0.Domain, error) {
	return bn.Bn.DomainData(epoch, domain)
}
func (bn *TestingBeaconNodeWrapped) SyncCommitteeSubnetID(index phase0.CommitteeIndex) (uint64, error) {
	return bn.Bn.SyncCommitteeSubnetID(index)
}
func (bn *TestingBeaconNodeWrapped) IsSyncCommitteeAggregator(proof []byte) (bool, error) {
	return bn.Bn.IsSyncCommitteeAggregator(proof)
}
func (bn *TestingBeaconNodeWrapped) GetSyncCommitteeContribution(slot phase0.Slot, selectionProofs []phase0.BLSSignature, subnetIDs []uint64) (ssz.Marshaler, spec.DataVersion, error) {
	return bn.Bn.GetSyncCommitteeContribution(slot, selectionProofs, subnetIDs)
}
func (bn *TestingBeaconNodeWrapped) SubmitAggregateSelectionProof(slot phase0.Slot, committeeIndex phase0.CommitteeIndex, committeeLength uint64, index phase0.ValidatorIndex, slotSig []byte) (ssz.Marshaler, spec.DataVersion, error) {
	return bn.Bn.SubmitAggregateSelectionProof(slot, committeeIndex, committeeLength, index, slotSig)
}
func (bn *TestingBeaconNodeWrapped) GetBeaconNetwork() spectypes.BeaconNetwork {
	return bn.Bn.GetBeaconNetwork()
}
func (bn *TestingBeaconNodeWrapped) GetBeaconBlock(slot phase0.Slot, graffiti, randao []byte) (ssz.Marshaler, spec.DataVersion, error) {
	return bn.Bn.GetBeaconBlock(slot, graffiti, randao)
}
func (bn *TestingBeaconNodeWrapped) SubmitValidatorRegistration(pubkey []byte, feeRecipient bellatrix.ExecutionAddress, sig phase0.BLSSignature) error {
	return bn.Bn.SubmitValidatorRegistration(pubkey, feeRecipient, sig)
}
func (bn *TestingBeaconNodeWrapped) SubmitVoluntaryExit(voluntaryExit *phase0.SignedVoluntaryExit) error {
	return bn.Bn.SubmitVoluntaryExit(voluntaryExit)
}
func (bn *TestingBeaconNodeWrapped) SubmitAttestations(attestations []*phase0.Attestation) error {
	return bn.Bn.SubmitAttestations(attestations)
}
func (bn *TestingBeaconNodeWrapped) SubmitSyncMessages(msgs []*altair.SyncCommitteeMessage) error {
	return bn.Bn.SubmitSyncMessages(msgs)
}
func (bn *TestingBeaconNodeWrapped) SubmitBlindedBeaconBlock(block *api.VersionedBlindedProposal, sig phase0.BLSSignature) error {
	return bn.Bn.SubmitBlindedBeaconBlock(block, sig)
}
func (bn *TestingBeaconNodeWrapped) SubmitSignedContributionAndProof(contribution *altair.SignedContributionAndProof) error {
	return bn.Bn.SubmitSignedContributionAndProof(contribution)
}
func (bn *TestingBeaconNodeWrapped) SubmitSignedAggregateSelectionProof(msg *phase0.SignedAggregateAndProof) error {
	return bn.Bn.SubmitSignedAggregateSelectionProof(msg)
}
func (bn *TestingBeaconNodeWrapped) SubmitBeaconBlock(block *api.VersionedProposal, sig phase0.BLSSignature) error {
	return bn.Bn.SubmitBeaconBlock(block, sig)
}

func NewTestingBeaconNodeWrapped() beacon.BeaconNode {
	bnw := &TestingBeaconNodeWrapped{}
	bnw.Bn = spectestingutils.NewTestingBeaconNode()

	return bnw
}
