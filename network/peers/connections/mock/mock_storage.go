package mock

import (
	"bytes"
	"math/big"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/operator/storage"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
)

var _ storage.Storage = NodeStorage{}

type NodeStorage struct {
	MockPrivateKeyHash              string
	MockPublicKey                   string
	RegisteredOperatorPublicKeyPEMs []string
}

func (m NodeStorage) Begin() basedb.Txn {
	panic("unexpected Begin call")
}

func (m NodeStorage) BeginRead() basedb.ReadTxn {
	panic("unexpected BeginRead call")
}

func (m NodeStorage) GetNextNonce(txn basedb.Reader, owner common.Address) (registrystorage.Nonce, error) {
	panic("unexpected GetNextNonce call")
}

func (m NodeStorage) BumpNonce(txn basedb.ReadWriter, owner common.Address) error {
	panic("unexpected BumpNonce call")
}

func (m NodeStorage) SaveLastProcessedBlock(txn basedb.ReadWriter, offset *big.Int) error {
	panic("unexpected SaveLastProcessedBlock call")
}

func (m NodeStorage) GetLastProcessedBlock(txn basedb.Reader) (*big.Int, bool, error) {
	panic("unexpected GetLastProcessedBlock call")
}

func (m NodeStorage) DropRegistryData() error {
	panic("unexpected DropRegistryData call")
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
	panic("unexpected GetOperatorData call")
}

func (m NodeStorage) OperatorsExist(r basedb.Reader, ids []spectypes.OperatorID) (bool, error) {
	panic("unexpected OperatorsExist call")
}

func (m NodeStorage) SaveOperatorData(txn basedb.ReadWriter, operatorData *registrystorage.OperatorData) (bool, error) {
	panic("unexpected SaveOperatorData call")
}

func (m NodeStorage) DeleteOperatorData(txn basedb.ReadWriter, id spectypes.OperatorID) error {
	panic("unexpected DeleteOperatorData call")
}

func (m NodeStorage) ListOperators(txn basedb.Reader, from uint64, to uint64) ([]registrystorage.OperatorData, error) {
	return nil, errors.New("empty")
}

func (m NodeStorage) GetOperatorsPrefix() []byte {
	panic("unexpected GetOperatorsPrefix call")
}

func (m NodeStorage) GetRecipientData(txn basedb.Reader, owner common.Address) (*registrystorage.RecipientData, bool, error) {
	panic("unexpected GetRecipientData call")
}

func (m NodeStorage) GetRecipientDataMany(txn basedb.Reader, owners []common.Address) (map[common.Address]bellatrix.ExecutionAddress, error) {
	panic("unexpected GetRecipientDataMany call")
}

func (m NodeStorage) SaveRecipientData(txn basedb.ReadWriter, recipientData *registrystorage.RecipientData) (*registrystorage.RecipientData, error) {
	panic("unexpected SaveRecipientData call")
}

func (m NodeStorage) DeleteRecipientData(txn basedb.ReadWriter, owner common.Address) error {
	panic("unexpected DeleteRecipientData call")
}

func (m NodeStorage) GetRecipientsPrefix() []byte {
	panic("unexpected GetRecipientsPrefix call")
}

func (m NodeStorage) Shares() registrystorage.Shares {
	panic("unexpected Shares call")
}

func (m NodeStorage) ValidatorStore() registrystorage.ValidatorStore {
	panic("unexpected ValidatorStore call")
}

func (m NodeStorage) DropOperators() error {
	panic("unexpected DropOperators call")
}

func (m NodeStorage) DropRecipients() error {
	panic("unexpected DropRecipients call")
}

func (m NodeStorage) DropShares() error {
	panic("unexpected DropShares call")
}

func (m NodeStorage) GetPrivateKeyHash() (string, bool, error) {
	if m.MockPrivateKeyHash != "" {
		return m.MockPrivateKeyHash, true, nil
	} else {
		return "", false, errors.New("error")
	}
}

func (m NodeStorage) SavePrivateKeyHash(privKeyHash string) error {
	panic("unexpected SavePrivateKeyHash call")
}

func (m NodeStorage) GetPublicKey() (string, bool, error) {
	if m.MockPublicKey != "" {
		return m.MockPublicKey, true, nil
	} else {
		return "", false, errors.New("error")
	}
}

func (m NodeStorage) SavePublicKey(publicKey string) error {
	panic("unexpected SavePublicKey call")
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
