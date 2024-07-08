package tests

import (
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
	bn *spectestingutils.TestingBeaconNode
}

func (bn *TestingBeaconNodeWrapped) SetSyncCommitteeAggregatorRootHexes(roots map[string]bool) {
	bn.bn.SetSyncCommitteeAggregatorRootHexes(roots)
}

func (bn *TestingBeaconNodeWrapped) GetBroadcastedRoots() []phase0.Root {
	return bn.bn.BroadcastedRoots
}

func (bn *TestingBeaconNodeWrapped) GetBeaconNode() *spectestingutils.TestingBeaconNode {
	return bn.bn
}

func (bn *TestingBeaconNodeWrapped) GetAttestationData(slot phase0.Slot, committeeIndex phase0.CommitteeIndex) (*phase0.AttestationData, spec.DataVersion, error) {
	return bn.bn.GetAttestationData(slot, committeeIndex)
}
func (bn *TestingBeaconNodeWrapped) DomainData(epoch phase0.Epoch, domain phase0.DomainType) (phase0.Domain, error) {
	return bn.bn.DomainData(epoch, domain)
}
func (bn *TestingBeaconNodeWrapped) SyncCommitteeSubnetID(index phase0.CommitteeIndex) (uint64, error) {
	return bn.bn.SyncCommitteeSubnetID(index)
}
func (bn *TestingBeaconNodeWrapped) IsSyncCommitteeAggregator(proof []byte) (bool, error) {
	return bn.bn.IsSyncCommitteeAggregator(proof)
}
func (bn *TestingBeaconNodeWrapped) GetSyncCommitteeContribution(slot phase0.Slot, selectionProofs []phase0.BLSSignature, subnetIDs []uint64) (ssz.Marshaler, spec.DataVersion, error) {
	return bn.bn.GetSyncCommitteeContribution(slot, selectionProofs, subnetIDs)
}
func (bn *TestingBeaconNodeWrapped) SubmitAggregateSelectionProof(slot phase0.Slot, committeeIndex phase0.CommitteeIndex, committeeLength uint64, index phase0.ValidatorIndex, slotSig []byte) (ssz.Marshaler, spec.DataVersion, error) {
	return bn.bn.SubmitAggregateSelectionProof(slot, committeeIndex, committeeLength, index, slotSig)
}
func (bn *TestingBeaconNodeWrapped) GetBeaconNetwork() spectypes.BeaconNetwork {
	return bn.bn.GetBeaconNetwork()
}
func (bn *TestingBeaconNodeWrapped) GetBeaconBlock(slot phase0.Slot, graffiti, randao []byte) (ssz.Marshaler, spec.DataVersion, error) {
	return bn.bn.GetBeaconBlock(slot, graffiti, randao)
}
func (bn *TestingBeaconNodeWrapped) SubmitValidatorRegistration(pubkey []byte, feeRecipient bellatrix.ExecutionAddress, sig phase0.BLSSignature) error {
	return bn.bn.SubmitValidatorRegistration(pubkey, feeRecipient, sig)
}
func (bn *TestingBeaconNodeWrapped) SubmitVoluntaryExit(voluntaryExit *phase0.SignedVoluntaryExit) error {
	return bn.bn.SubmitVoluntaryExit(voluntaryExit)
}
func (bn *TestingBeaconNodeWrapped) SubmitAttestations(attestations []*phase0.Attestation) error {
	return bn.bn.SubmitAttestations(attestations)
}
func (bn *TestingBeaconNodeWrapped) SubmitSyncMessages(msgs []*altair.SyncCommitteeMessage) error {
	return bn.bn.SubmitSyncMessages(msgs)
}

func NewTestingBeaconNodeWrapped() beacon.BeaconNode {
	bnw := &TestingBeaconNodeWrapped{}
	bnw.bn = spectestingutils.NewTestingBeaconNode()

	return bnw
}
