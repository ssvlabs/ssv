package beacon

import (
	v1 "github.com/attestantio/go-eth2-client/api/v1"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/herumi/bls-eth-go-binary/bls"
	"sync"
)

func init() {
	_ = bls.Init(bls.BLS12_381)
}

// NewMockBeacon creates a new mock implementation of beacon client
func NewMockBeacon(dutiesResults map[uint64][]*Duty, validatorsData map[spec.BLSPubKey]*v1.Validator) Beacon {
	return &mockBeacon{
		indicesMap:     map[spec.BLSPubKey]spec.ValidatorIndex{},
		indicesLock:    sync.Mutex{},
		dutiesResults:  dutiesResults,
		validatorsData: validatorsData,
	}
}

type mockBeacon struct {
	indicesMap     map[spec.BLSPubKey]spec.ValidatorIndex
	indicesLock    sync.Mutex
	dutiesResults  map[uint64][]*Duty
	validatorsData map[spec.BLSPubKey]*v1.Validator
}

func (m *mockBeacon) ExtendIndexMap(index spec.ValidatorIndex, pubKey spec.BLSPubKey) {
	m.indicesLock.Lock()
	defer m.indicesLock.Unlock()

	m.indicesMap[pubKey] = index
}

func (m *mockBeacon) GetDuties(epoch spec.Epoch, validatorIndices []spec.ValidatorIndex) ([]*Duty, error) {
	return m.dutiesResults[uint64(epoch)], nil
}

func (m *mockBeacon) GetValidatorData(validatorPubKeys []spec.BLSPubKey) (map[spec.ValidatorIndex]*v1.Validator, error) {
	results := map[spec.ValidatorIndex]*v1.Validator{}
	for _, pk := range validatorPubKeys {
		if data, ok := m.validatorsData[pk]; ok {
			results[data.Index] = data
		}
	}
	return results, nil
}

func (m *mockBeacon) GetAttestationData(slot spec.Slot, committeeIndex spec.CommitteeIndex) (*spec.AttestationData, error) {
	return nil, nil
}

func (m *mockBeacon) SignAttestation(data *spec.AttestationData, duty *Duty, pk []byte) (*spec.Attestation, []byte, error) {
	return nil, nil, nil
}

func (m *mockBeacon) SubmitAttestation(attestation *spec.Attestation) error {
	return nil
}

func (m *mockBeacon) SubscribeToCommitteeSubnet(subscription []*v1.BeaconCommitteeSubscription) error {
	return nil
}

func (m *mockBeacon) AddShare(shareKey *bls.SecretKey) error {
	return nil
}

func (m *mockBeacon) RemoveShare(pubKey string) error {
	return nil
}

func (m *mockBeacon) SignIBFTMessage(message *proto.Message, pk []byte) ([]byte, error) {
	return nil, nil
}

func (m *mockBeacon) GetDomain(data *spec.AttestationData) ([]byte, error) {
	panic("implement")
}
func (m *mockBeacon) ComputeSigningRoot(object interface{}, domain []byte) ([32]byte, error) {
	panic("implement")
}

// NewMockValidatorMetadataStorage creates a new mock implementation of ValidatorMetadataStorage
func NewMockValidatorMetadataStorage() ValidatorMetadataStorage {
	return &mockValidatorMetadataStorage{
		map[string]*ValidatorMetadata{},
		sync.Mutex{},
	}
}

type mockValidatorMetadataStorage struct {
	data map[string]*ValidatorMetadata
	lock sync.Mutex
}

func (m *mockValidatorMetadataStorage) UpdateValidatorMetadata(pk string, metadata *ValidatorMetadata) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.data[pk] = metadata

	return nil
}

func (m *mockValidatorMetadataStorage) Size() int {
	m.lock.Lock()
	defer m.lock.Unlock()

	return len(m.data)
}
