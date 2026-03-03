package ekm

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/eth2-key-manager/core"
	"github.com/stretchr/testify/mock"

	ssvclient "github.com/ssvlabs/ssv/ssvsigner"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

type MockRemoteSigner struct {
	mock.Mock
}

func (m *MockRemoteSigner) AddValidators(ctx context.Context, shares ...ssvclient.ShareKeys) ([]web3signer.Status, error) {
	args := m.Called(ctx, shares[0])
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]web3signer.Status), args.Error(1)
}

func (m *MockRemoteSigner) RemoveValidators(ctx context.Context, pubKeys ...phase0.BLSPubKey) ([]web3signer.Status, error) {
	args := m.Called(ctx, pubKeys)
	result := args.Get(0)
	if result == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]web3signer.Status), args.Error(1)
}

func (m *MockRemoteSigner) Sign(ctx context.Context, sharePubKey phase0.BLSPubKey, payload web3signer.SignRequest) (phase0.BLSSignature, error) {
	args := m.Called(ctx, sharePubKey, payload)
	return args.Get(0).(phase0.BLSSignature), args.Error(1)
}

func (m *MockRemoteSigner) OperatorIdentity(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}

func (m *MockRemoteSigner) OperatorSign(ctx context.Context, payload []byte) ([]byte, error) {
	args := m.Called(ctx, payload)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]byte), args.Error(1)
}

type MockBeaconNetwork struct {
	mock.Mock
}

type MockDatabase struct {
	mock.Mock
}

func (m *MockDatabase) Begin() ReadWriteTxn {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(ReadWriteTxn)
}

func (m *MockDatabase) BeginRead() ReadTxn {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(ReadTxn)
}

func (m *MockDatabase) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockDatabase) Get(txn ReadTxn, prefix []byte, key []byte) (Obj, bool, error) {
	args := m.Called(txn, prefix, key)
	if args.Get(0) == nil {
		return Obj{}, args.Bool(1), args.Error(2)
	}
	return args.Get(0).(Obj), args.Bool(1), args.Error(2)
}

func (m *MockDatabase) Set(txn ReadWriteTxn, prefix []byte, key []byte, value []byte) error {
	args := m.Called(txn, prefix, key, value)
	return args.Error(0)
}

func (m *MockDatabase) Delete(txn ReadWriteTxn, prefix []byte, key []byte) error {
	args := m.Called(txn, prefix, key)
	return args.Error(0)
}

func (m *MockDatabase) GetAll(txn ReadTxn, prefix []byte, handler func(int, Obj) error) error {
	args := m.Called(txn, prefix, handler)
	return args.Error(0)
}

func (m *MockDatabase) DropPrefix(prefix []byte) error {
	return nil
}

type MockOperatorPublicKey struct {
	mock.Mock
}

func (m *MockOperatorPublicKey) Encrypt(data []byte) ([]byte, error) {
	args := m.Called(data)
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockOperatorPublicKey) Verify(data []byte, signature []byte) error {
	args := m.Called(data, signature)
	return args.Error(0)
}

func (m *MockOperatorPublicKey) Base64() (string, error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}

type MockSlashingProtector struct {
	mock.Mock
}

func (m *MockSlashingProtector) ListAccounts() ([]core.ValidatorAccount, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]core.ValidatorAccount), args.Error(1)
}

func (m *MockSlashingProtector) RetrieveHighestAttestation(pubKey phase0.BLSPubKey) (*phase0.AttestationData, bool, error) {
	args := m.Called(pubKey)
	if args.Get(0) == nil {
		return nil, args.Bool(1), args.Error(2)
	}
	return args.Get(0).(*phase0.AttestationData), args.Bool(1), args.Error(2)
}

func (m *MockSlashingProtector) RetrieveHighestProposal(pubKey phase0.BLSPubKey) (phase0.Slot, bool, error) {
	args := m.Called(pubKey)
	return args.Get(0).(phase0.Slot), args.Bool(1), args.Error(2)
}

func (m *MockSlashingProtector) IsAttestationSlashable(pk phase0.BLSPubKey, data *phase0.AttestationData) error {
	args := m.Called(pk, data)
	return args.Error(0)
}

func (m *MockSlashingProtector) UpdateHighestAttestation(pubKey phase0.BLSPubKey, attestation *phase0.AttestationData) error {
	args := m.Called(pubKey, attestation)
	return args.Error(0)
}

func (m *MockSlashingProtector) IsBeaconBlockSlashable(pubKey phase0.BLSPubKey, slot phase0.Slot) error {
	args := m.Called(pubKey, slot)
	return args.Error(0)
}

func (m *MockSlashingProtector) UpdateHighestProposal(pubKey phase0.BLSPubKey, slot phase0.Slot) error {
	args := m.Called(pubKey, slot)
	return args.Error(0)
}

func (m *MockSlashingProtector) BumpSlashingProtectionTxn(txn ReadWriteTxn, pubKey phase0.BLSPubKey) error {
	args := m.Called(txn, pubKey)
	return args.Error(0)
}

func (m *MockSlashingProtector) RemoveHighestAttestationTxn(txn ReadWriteTxn, pubKey phase0.BLSPubKey) error {
	args := m.Called(txn, pubKey)
	return args.Error(0)
}

func (m *MockSlashingProtector) RemoveHighestProposalTxn(txn ReadWriteTxn, pubKey phase0.BLSPubKey) error {
	args := m.Called(txn, pubKey)
	return args.Error(0)
}
