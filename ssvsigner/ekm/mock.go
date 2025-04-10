package ekm

import (
	"context"

	eth2api "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/eth2-key-manager/core"
	"github.com/stretchr/testify/mock"

	ssvclient "github.com/ssvlabs/ssv/ssvsigner"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"

	"github.com/ssvlabs/ssv/storage/basedb"
)

type MockRemoteSigner struct {
	mock.Mock
}

func (m *MockRemoteSigner) AddValidators(ctx context.Context, shares ...ssvclient.ShareKeys) error {
	args := m.Called(ctx, shares[0])
	if args.Get(0) == nil {
		return args.Error(0)
	}
	return args.Error(0)
}

func (m *MockRemoteSigner) RemoveValidators(ctx context.Context, pubKeys ...phase0.BLSPubKey) error {
	args := m.Called(ctx, pubKeys)
	result := args.Get(0)
	if result == nil {
		return args.Error(0)
	}
	return args.Error(0)
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

type MockConsensusClient struct {
	mock.Mock
}

func (m *MockConsensusClient) ForkAtEpoch(ctx context.Context, epoch phase0.Epoch) (*phase0.Fork, error) {
	args := m.Called(ctx, epoch)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*phase0.Fork), args.Error(1)
}

func (m *MockConsensusClient) Genesis(ctx context.Context) (*eth2api.Genesis, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*eth2api.Genesis), args.Error(1)
}

type MockBeaconNetwork struct {
	mock.Mock
}

type MockDatabase struct {
	mock.Mock
}

func (m *MockDatabase) Begin() basedb.Txn {
	args := m.Called()
	return args.Get(0).(basedb.Txn)
}

func (m *MockDatabase) BeginRead() basedb.ReadTxn {
	args := m.Called()
	return args.Get(0).(basedb.ReadTxn)
}

func (m *MockDatabase) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockDatabase) Get(prefix []byte, key []byte) (basedb.Obj, bool, error) {
	args := m.Called(prefix, key)
	return args.Get(0).(basedb.Obj), args.Bool(1), args.Error(2)
}

func (m *MockDatabase) Set(prefix []byte, key []byte, value []byte) error {
	args := m.Called(prefix, key, value)
	return args.Error(0)
}

func (m *MockDatabase) Delete(prefix []byte, key []byte) error {
	args := m.Called(prefix, key)
	return args.Error(0)
}

func (m *MockDatabase) GetMany(prefix []byte, keys [][]byte, iterator func(basedb.Obj) error) error {
	return nil
}

func (m *MockDatabase) GetAll(prefix []byte, handler func(int, basedb.Obj) error) error {
	return nil
}

func (m *MockDatabase) SetMany(prefix []byte, n int, next func(int) (basedb.Obj, error)) error {
	return nil
}

func (m *MockDatabase) Using(rw basedb.ReadWriter) basedb.ReadWriter {
	return nil
}

func (m *MockDatabase) UsingReader(r basedb.Reader) basedb.Reader {
	return nil
}

func (m *MockDatabase) CountPrefix(prefix []byte) (int64, error) {
	return 0, nil
}

func (m *MockDatabase) DropPrefix(prefix []byte) error {
	return nil
}

func (m *MockDatabase) Update(fn func(basedb.Txn) error) error {
	return nil
}

type MockTxn struct {
	mock.Mock
}

func (m *MockTxn) Commit() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockTxn) Discard() {
	m.Called()
}

func (m *MockTxn) Get(prefix []byte, key []byte) (basedb.Obj, bool, error) {
	args := m.Called(prefix, key)
	return args.Get(0).(basedb.Obj), args.Bool(1), args.Error(2)
}

func (m *MockTxn) Set(prefix []byte, key []byte, value []byte) error {
	args := m.Called(prefix, key, value)
	return args.Error(0)
}

func (m *MockTxn) Delete(prefix []byte, key []byte) error {
	args := m.Called(prefix, key)
	return args.Error(0)
}

func (m *MockTxn) GetMany(prefix []byte, keys [][]byte, iterator func(basedb.Obj) error) error {
	return nil
}

func (m *MockTxn) GetAll(prefix []byte, handler func(int, basedb.Obj) error) error {
	return nil
}

func (m *MockTxn) SetMany(prefix []byte, n int, next func(int) (basedb.Obj, error)) error {
	return nil
}

type MockReadTxn struct {
	mock.Mock
}

func (m *MockReadTxn) Discard() {
	m.Called()
}

func (m *MockReadTxn) Get(prefix []byte, key []byte) (basedb.Obj, bool, error) {
	args := m.Called(prefix, key)
	return args.Get(0).(basedb.Obj), args.Bool(1), args.Error(2)
}

func (m *MockReadTxn) GetMany(prefix []byte, keys [][]byte, iterator func(basedb.Obj) error) error {
	return nil
}

func (m *MockReadTxn) GetAll(prefix []byte, handler func(int, basedb.Obj) error) error {
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

func (m *MockSlashingProtector) BumpSlashingProtection(pubKey phase0.BLSPubKey) error {
	args := m.Called(pubKey)
	return args.Error(0)
}

func (m *MockSlashingProtector) RemoveHighestAttestation(pubKey phase0.BLSPubKey) error {
	args := m.Called(pubKey)
	return args.Error(0)
}

func (m *MockSlashingProtector) RemoveHighestProposal(pubKey phase0.BLSPubKey) error {
	args := m.Called(pubKey)
	return args.Error(0)
}
