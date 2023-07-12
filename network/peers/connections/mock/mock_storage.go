package mock

import (
	"bytes"
	"crypto/rsa"
	"math/big"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/operator/storage"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
)

var _ storage.Storage = NodeStorage{}

type NodeStorage struct {
	MockGetPrivateKey               *rsa.PrivateKey
	RegisteredOperatorPublicKeyPEMs []string
}

func (m NodeStorage) GetEventData(txHash common.Hash) (*registrystorage.EventData, bool, error) {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) SaveEventData(txHash common.Hash) error {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) GetNextNonce(owner common.Address) (registrystorage.Nonce, error) {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) BumpNonce(owner common.Address) error {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) GetEventsPrefix() []byte {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) SaveSyncOffset(offset *big.Int) error {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) GetSyncOffset() (*big.Int, bool, error) {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) CleanRegistryData() error {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) GetOperatorDataByPubKey(logger *zap.Logger, operatorPublicKeyPEM []byte) (*registrystorage.OperatorData, bool, error) {
	for _, current := range m.RegisteredOperatorPublicKeyPEMs {
		if bytes.Equal([]byte(current), operatorPublicKeyPEM) {
			return &registrystorage.OperatorData{}, true, nil
		}
	}

	return nil, false, errors.New("operator not found")
}

func (m NodeStorage) GetOperatorData(id spectypes.OperatorID) (*registrystorage.OperatorData, bool, error) {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) SaveOperatorData(logger *zap.Logger, operatorData *registrystorage.OperatorData) (bool, error) {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) DeleteOperatorData(id spectypes.OperatorID) error {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) ListOperators(logger *zap.Logger, from uint64, to uint64) ([]registrystorage.OperatorData, error) {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) GetOperatorsPrefix() []byte {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) GetRecipientData(owner common.Address) (*registrystorage.RecipientData, bool, error) {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) GetRecipientDataMany(logger *zap.Logger, owners []common.Address) (map[common.Address]bellatrix.ExecutionAddress, error) {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) SaveRecipientData(recipientData *registrystorage.RecipientData) (*registrystorage.RecipientData, error) {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) DeleteRecipientData(owner common.Address) error {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) GetRecipientsPrefix() []byte {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) Shares() registrystorage.Shares {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) GetPrivateKey() (*rsa.PrivateKey, bool, error) {
	if m.MockGetPrivateKey != nil {
		return m.MockGetPrivateKey, true, nil
	} else {
		return nil, false, errors.New("error")
	}
}

func (m NodeStorage) SetupPrivateKey(logger *zap.Logger, operatorKeyBase64 string, generateIfNone bool) ([]byte, error) {
	//TODO implement me
	panic("implement me")
}
