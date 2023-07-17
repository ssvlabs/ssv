package mock

import (
	"bytes"
	"crypto/rsa"
	"math/big"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"

	"github.com/bloxapp/ssv/operator/storage"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/bloxapp/ssv/storage/basedb"
)

var _ storage.Storage = NodeStorage{}

type NodeStorage struct {
	MockGetPrivateKey               *rsa.PrivateKey
	RegisteredOperatorPublicKeyPEMs []string
}

func (m NodeStorage) ROTxn() basedb.Txn {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) RWTxn() basedb.Txn {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) GetEventData(txHash common.Hash) (*registrystorage.EventData, bool, error) {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) SaveEventData(txHash common.Hash) error {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) GetNextNonce(txn basedb.Txn, owner common.Address) (registrystorage.Nonce, error) {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) BumpNonce(txn basedb.Txn, owner common.Address) error {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) GetEventsPrefix() []byte {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) SaveLastProcessedBlock(txn basedb.Txn, offset *big.Int) error {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) GetLastProcessedBlock(txn basedb.Txn) (*big.Int, bool, error) {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) CleanRegistryData() error {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) GetOperatorDataByPubKey(txn basedb.Txn, operatorPublicKeyPEM []byte) (*registrystorage.OperatorData, bool, error) {
	for _, current := range m.RegisteredOperatorPublicKeyPEMs {
		if bytes.Equal([]byte(current), operatorPublicKeyPEM) {
			return &registrystorage.OperatorData{}, true, nil
		}
	}

	return nil, false, errors.New("operator not found")
}

func (m NodeStorage) GetOperatorData(txn basedb.Txn, id spectypes.OperatorID) (*registrystorage.OperatorData, bool, error) {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) SaveOperatorData(txn basedb.Txn, operatorData *registrystorage.OperatorData) (bool, error) {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) DeleteOperatorData(txn basedb.Txn, id spectypes.OperatorID) error {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) ListOperators(txn basedb.Txn, from uint64, to uint64) ([]registrystorage.OperatorData, error) {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) GetOperatorsPrefix() []byte {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) GetRecipientData(txn basedb.Txn, owner common.Address) (*registrystorage.RecipientData, bool, error) {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) GetRecipientDataMany(txn basedb.Txn, owners []common.Address) (map[common.Address]bellatrix.ExecutionAddress, error) {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) SaveRecipientData(txn basedb.Txn, recipientData *registrystorage.RecipientData) (*registrystorage.RecipientData, error) {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) DeleteRecipientData(txn basedb.Txn, owner common.Address) error {
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

func (m NodeStorage) SetupPrivateKey(operatorKeyBase64 string, generateIfNone bool) ([]byte, error) {
	//TODO implement me
	panic("implement me")
}
