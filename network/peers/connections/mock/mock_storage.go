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

func (m NodeStorage) Begin() basedb.Txn {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) BeginRead() basedb.ReadTxn {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) GetNextNonce(txn basedb.Reader, owner common.Address) (registrystorage.Nonce, error) {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) BumpNonce(txn basedb.ReadWriter, owner common.Address) error {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) SaveLastProcessedBlock(txn basedb.ReadWriter, offset *big.Int) error {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) GetLastProcessedBlock(txn basedb.Reader) (*big.Int, bool, error) {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) DropRegistryData() error {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) GetOperatorDataByPubKey(txn basedb.Reader, operatorPublicKeyPEM []byte) (*registrystorage.OperatorData, bool, error) {
	for _, current := range m.RegisteredOperatorPublicKeyPEMs {
		if bytes.Equal([]byte(current), operatorPublicKeyPEM) {
			return &registrystorage.OperatorData{}, true, nil
		}
	}

	return nil, false, errors.New("operator not found")
}

func (m NodeStorage) GetOperatorData(txn basedb.Reader, id spectypes.OperatorID) (*registrystorage.OperatorData, bool, error) {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) OperatorsExist(r basedb.Reader, ids []spectypes.OperatorID) (bool, error) {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) SaveOperatorData(txn basedb.ReadWriter, operatorData *registrystorage.OperatorData) (bool, error) {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) DeleteOperatorData(txn basedb.ReadWriter, id spectypes.OperatorID) error {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) ListOperators(txn basedb.Reader, from uint64, to uint64) ([]registrystorage.OperatorData, error) {
	//TODO implement me
	return nil, errors.New("empty")
}

func (m NodeStorage) GetOperatorsPrefix() []byte {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) GetRecipientData(txn basedb.Reader, owner common.Address) (*registrystorage.RecipientData, bool, error) {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) GetRecipientDataMany(txn basedb.Reader, owners []common.Address) (map[common.Address]bellatrix.ExecutionAddress, error) {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) SaveRecipientData(txn basedb.ReadWriter, recipientData *registrystorage.RecipientData) (*registrystorage.RecipientData, error) {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) DeleteRecipientData(txn basedb.ReadWriter, owner common.Address) error {
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

func (m NodeStorage) DropOperators() error {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) DropRecipients() error {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) DropShares() error {
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

func (m NodeStorage) SetupPrivateKey(operatorKeyBase64 string) ([]byte, error) {
	//TODO implement me
	panic("implement me")
}

func (m NodeStorage) GetConfig(rw basedb.ReadWriter) (*storage.ConfigLock, bool, error) {
	panic("implement me")
}

func (m NodeStorage) SaveConfig(rw basedb.ReadWriter, config *storage.ConfigLock) error {
	panic("implement me")
}

func (m NodeStorage) DeleteConfig(rw basedb.ReadWriter) error {
	panic("implement me")
}
