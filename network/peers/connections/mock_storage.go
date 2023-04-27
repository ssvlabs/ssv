package connections

import (
	"bytes"
	"crypto/rsa"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/protocol/v2/types"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

var _ storage.Storage = MockStorage{}

type MockStorage struct {
	RegisteredOperatorPublicKeyPEMs [][]byte
}

func (m MockStorage) SaveSyncOffset(offset *eth1.SyncOffset) error {
	//TODO implement me
	panic("implement me")
}

func (m MockStorage) GetSyncOffset() (*eth1.SyncOffset, bool, error) {
	//TODO implement me
	panic("implement me")
}

func (m MockStorage) CleanRegistryData() error {
	//TODO implement me
	panic("implement me")
}

func (m MockStorage) GetOperatorDataByPubKey(logger *zap.Logger, operatorPublicKeyPEM []byte) (*registrystorage.OperatorData, bool, error) {
	for _, current := range m.RegisteredOperatorPublicKeyPEMs {
		if bytes.Equal(current, operatorPublicKeyPEM) {
			return &registrystorage.OperatorData{}, true, nil
		}
	}

	return nil, false, errors.New("operator not found")
}

func (m MockStorage) GetOperatorData(id spectypes.OperatorID) (*registrystorage.OperatorData, bool, error) {
	//TODO implement me
	panic("implement me")
}

func (m MockStorage) SaveOperatorData(logger *zap.Logger, operatorData *registrystorage.OperatorData) (bool, error) {
	//TODO implement me
	panic("implement me")
}

func (m MockStorage) DeleteOperatorData(id spectypes.OperatorID) error {
	//TODO implement me
	panic("implement me")
}

func (m MockStorage) ListOperators(logger *zap.Logger, from uint64, to uint64) ([]registrystorage.OperatorData, error) {
	//TODO implement me
	panic("implement me")
}

func (m MockStorage) GetOperatorsPrefix() []byte {
	//TODO implement me
	panic("implement me")
}

func (m MockStorage) GetRecipientData(owner common.Address) (*registrystorage.RecipientData, bool, error) {
	//TODO implement me
	panic("implement me")
}

func (m MockStorage) GetRecipientDataMany(logger *zap.Logger, owners []common.Address) (map[common.Address]bellatrix.ExecutionAddress, error) {
	//TODO implement me
	panic("implement me")
}

func (m MockStorage) SaveRecipientData(recipientData *registrystorage.RecipientData) (*registrystorage.RecipientData, error) {
	//TODO implement me
	panic("implement me")
}

func (m MockStorage) DeleteRecipientData(owner common.Address) error {
	//TODO implement me
	panic("implement me")
}

func (m MockStorage) GetRecipientsPrefix() []byte {
	//TODO implement me
	panic("implement me")
}

func (m MockStorage) SaveShare(logger *zap.Logger, share *types.SSVShare) error {
	//TODO implement me
	panic("implement me")
}

func (m MockStorage) SaveShareMany(logger *zap.Logger, shares []*types.SSVShare) error {
	//TODO implement me
	panic("implement me")
}

func (m MockStorage) GetShare(key []byte) (*types.SSVShare, bool, error) {
	//TODO implement me
	panic("implement me")
}

func (m MockStorage) GetAllShares(logger *zap.Logger) ([]*types.SSVShare, error) {
	//TODO implement me
	panic("implement me")
}

func (m MockStorage) GetFilteredShares(logger *zap.Logger, f func(share *types.SSVShare) bool) ([]*types.SSVShare, error) {
	//TODO implement me
	panic("implement me")
}

func (m MockStorage) DeleteShare(key []byte) error {
	//TODO implement me
	panic("implement me")
}

func (m MockStorage) GetPrivateKey() (*rsa.PrivateKey, bool, error) {
	//TODO implement me
	panic("implement me")
}

func (m MockStorage) SetupPrivateKey(logger *zap.Logger, operatorKeyBase64 string, generateIfNone bool) ([]byte, error) {
	//TODO implement me
	panic("implement me")
}
