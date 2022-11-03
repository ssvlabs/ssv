package commons

import (
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	qbft2 "github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/ssv"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
)

func NewQBFTStorageAdapter(store qbftstorage.QBFTStore) qbft2.Storage {
	return &storageAdapter{store: store}
}

type storageAdapter struct {
	store qbftstorage.QBFTStore
}

// SaveHighestDecided saves (and potentially overrides) the highest Decided for a specific instance
func (sa *storageAdapter) SaveHighestDecided(signedMsg *qbft2.SignedMessage) error {
	return sa.store.SaveLastDecided(signedMsg)
}

// GetHighestDecided returns highest decided if found, nil if didn't
func (sa *storageAdapter) GetHighestDecided(identifier []byte) (*qbft2.SignedMessage, error) {
	return sa.store.GetLastDecided(identifier)
}

type beaconAdapter struct {
	beacon beacon.Beacon
}

func NewBeaconAdapter(beacon beacon.Beacon) ssv.BeaconNode {
	return beaconAdapter{beacon: beacon}
}

func (b beaconAdapter) GetBeaconNetwork() types.BeaconNetwork {
	//TODO implement me
	panic("implement me")
}

func (b beaconAdapter) GetAttestationData(slot spec.Slot, committeeIndex spec.CommitteeIndex) (*spec.AttestationData, error) {
	return b.beacon.GetAttestationData(slot, committeeIndex)
}

func (b beaconAdapter) SubmitAttestation(attestation *spec.Attestation) error {
	return b.beacon.SubmitAttestation(attestation)
}

func (b beaconAdapter) GetBeaconBlock(slot spec.Slot, committeeIndex spec.CommitteeIndex, graffiti, randao []byte) (*bellatrix.BeaconBlock, error) {
	//TODO implement me
	panic("implement me")
}

func (b beaconAdapter) SubmitBeaconBlock(block *bellatrix.SignedBeaconBlock) error {
	//TODO implement me
	panic("implement me")
}

func (b beaconAdapter) SubmitAggregateSelectionProof(slot spec.Slot, committeeIndex spec.CommitteeIndex, slotSig []byte) (*spec.AggregateAndProof, error) {
	//TODO implement me
	panic("implement me")
}

func (b beaconAdapter) SubmitSignedAggregateSelectionProof(msg *spec.SignedAggregateAndProof) error {
	//TODO implement me
	panic("implement me")
}

func (b beaconAdapter) GetSyncMessageBlockRoot() (spec.Root, error) {
	//TODO implement me
	panic("implement me")
}

func (b beaconAdapter) SubmitSyncMessage(msg *altair.SyncCommitteeMessage) error {
	//TODO implement me
	panic("implement me")
}

func (b beaconAdapter) GetSyncSubcommitteeIndex(slot spec.Slot, pubKey spec.BLSPubKey) ([]uint64, error) {
	//TODO implement me
	panic("implement me")
}

func (b beaconAdapter) IsSyncCommitteeAggregator(proof []byte) (bool, error) {
	//TODO implement me
	panic("implement me")
}

func (b beaconAdapter) SyncCommitteeSubnetID(subCommitteeID uint64) (uint64, error) {
	//TODO implement me
	panic("implement me")
}

func (b beaconAdapter) GetSyncCommitteeContribution(slot spec.Slot, subnetID uint64, pubKey spec.BLSPubKey) (*altair.SyncCommitteeContribution, error) {
	//TODO implement me
	panic("implement me")
}

func (b beaconAdapter) SubmitSignedContributionAndProof(contribution *altair.SignedContributionAndProof) error {
	//TODO implement me
	panic("implement me")
}

func (b beaconAdapter) DomainData(epoch spec.Epoch, domain spec.DomainType) (spec.Domain, error) {
	// epoch is used to calculate fork version, here we hard code it
	//return types.ComputeETHDomain(domain, types.GenesisForkVersion, types.GenesisValidatorsRoot)

	d, err := b.beacon.GetDomain(&spec.AttestationData{Slot: spec.Slot(epoch * 32)}) // TODO by default using Attester type. need to adjust
	var d32 spec.Domain
	copy(d32[:], d[:])
	return d32, err
}
