package beacon

import (
	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	fssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/go-ssz"
)

// TODO need to use mockgen instead
type beaconMock struct {
}

func NewBeaconMock() Beacon {
	return beaconMock{}
}

func (b beaconMock) GetBeaconNetwork() spectypes.BeaconNetwork {
	//TODO implement me
	panic("implement me")
}

func (b beaconMock) GetAttestationData(slot phase0.Slot, committeeIndex phase0.CommitteeIndex) (*phase0.AttestationData, error) {
	//TODO implement me
	panic("implement me")
}

func (b beaconMock) SubmitAttestation(attestation *phase0.Attestation) error {
	//TODO implement me
	panic("implement me")
}

func (b beaconMock) GetBeaconBlock(slot phase0.Slot, graffiti, randao []byte) (*bellatrix.BeaconBlock, error) {
	//TODO implement me
	panic("implement me")
}

func (b beaconMock) SubmitBeaconBlock(block *bellatrix.SignedBeaconBlock) error {
	//TODO implement me
	panic("implement me")
}

func (b beaconMock) SubmitAggregateSelectionProof(slot phase0.Slot, committeeIndex phase0.CommitteeIndex, committeeLength uint64, index phase0.ValidatorIndex, slotSig []byte) (*phase0.AggregateAndProof, error) {
	//TODO implement me
	panic("implement me")
}

func (b beaconMock) SubmitSignedAggregateSelectionProof(msg *phase0.SignedAggregateAndProof) error {
	//TODO implement me
	panic("implement me")
}

func (b beaconMock) GetSyncMessageBlockRoot(slot phase0.Slot) (phase0.Root, error) {
	//TODO implement me
	panic("implement me")
}

func (b beaconMock) SubmitSyncMessage(msg *altair.SyncCommitteeMessage) error {
	//TODO implement me
	panic("implement me")
}

func (b beaconMock) IsSyncCommitteeAggregator(proof []byte) (bool, error) {
	//TODO implement me
	panic("implement me")
}

func (b beaconMock) SyncCommitteeSubnetID(index phase0.CommitteeIndex) (uint64, error) {
	//TODO implement me
	panic("implement me")
}

func (b beaconMock) GetSyncCommitteeContribution(slot phase0.Slot, subnetID uint64) (*altair.SyncCommitteeContribution, error) {
	//TODO implement me
	panic("implement me")
}

func (b beaconMock) SubmitSignedContributionAndProof(contribution *altair.SignedContributionAndProof) error {
	//TODO implement me
	panic("implement me")
}

func (b beaconMock) DomainData(epoch phase0.Epoch, domain phase0.DomainType) (phase0.Domain, error) {
	return phase0.Domain{}, nil
}

func (b beaconMock) GetDuties(epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*spectypes.Duty, error) {
	//TODO implement me
	panic("implement me")
}

func (b beaconMock) SubscribeToCommitteeSubnet(subscription []*v1.BeaconCommitteeSubscription) error {
	//TODO implement me
	panic("implement me")
}

func (b beaconMock) SubmitSyncCommitteeSubscriptions(subscription []*v1.SyncCommitteeSubscription) error {
	//TODO implement me
	panic("implement me")
}

func (b beaconMock) GetValidatorData(validatorPubKeys []phase0.BLSPubKey) (map[phase0.ValidatorIndex]*v1.Validator, error) {
	//TODO implement me
	panic("implement me")
}

func (b beaconMock) ComputeSigningRoot(object interface{}, domain phase0.Domain) ([32]byte, error) {
	if object == nil {
		return [32]byte{}, errors.New("cannot compute signing root of nil")
	}
	return b.signingData(func() ([32]byte, error) {
		if v, ok := object.(fssz.HashRoot); ok {
			return v.HashTreeRoot()
		}
		return ssz.HashTreeRoot(object)
	}, domain[:])
}

func (b beaconMock) signingData(rootFunc func() ([32]byte, error), domain []byte) ([32]byte, error) {
	objRoot, err := rootFunc()
	if err != nil {
		return [32]byte{}, err
	}
	root := phase0.Root{}
	copy(root[:], objRoot[:])
	_domain := phase0.Domain{}
	copy(_domain[:], domain)
	container := &phase0.SigningData{
		ObjectRoot: root,
		Domain:     _domain,
	}
	return container.HashTreeRoot()
}
