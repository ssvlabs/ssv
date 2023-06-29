package eventdb

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/dgraph-io/badger/v4"
	ethcommon "github.com/ethereum/go-ethereum/common"

	registrystorage "github.com/bloxapp/ssv/registry/storage"
)

type ROTxn struct {
	txn *badger.Txn
}

func NewROTxn(txn *badger.Txn) *ROTxn {
	return &ROTxn{txn: txn}
}

func (t *ROTxn) Discard() {
	t.txn.Discard()
}

func (t *ROTxn) Commit() error {
	return t.txn.Commit()
}

// GetLastProcessedBlock returns last processed block from DB or nil if it doesn't exist.
func (t *ROTxn) GetLastProcessedBlock() (*big.Int, error) {
	blockItem, err := t.txn.Get([]byte(storagePrefix + lastProcessedBlockKey))
	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get item: %w", err)
	}

	blockValue, err := blockItem.ValueCopy(nil)
	if err != nil {
		return nil, fmt.Errorf("copy value: %w", err)
	}

	return new(big.Int).SetBytes(blockValue), nil
}

// GetOperatorData returns *registrystorage.OperatorData by given ID and nil if not found.
func (t *ROTxn) GetOperatorData(id uint64) (*registrystorage.OperatorData, error) {
	rawItem, err := t.txn.Get([]byte(fmt.Sprintf("%s%s/%d", storagePrefix, operatorsPrefix, id)))
	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get item: %w", err)
	}

	rawJSON, err := rawItem.ValueCopy(nil)
	if err != nil {
		return nil, fmt.Errorf("raw copy: %w", err)
	}

	var od registrystorage.OperatorData
	if err := json.Unmarshal(rawJSON, &od); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	return &od, nil
}

func (t *ROTxn) GetRecipientData(owner ethcommon.Address) (*RecipientData, error) {
	rawItem, err := t.txn.Get(append([]byte(fmt.Sprintf("%s%s/", storagePrefix, recipientsPrefix)), owner.Bytes()...))
	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	rawJSON, err := rawItem.ValueCopy(nil)
	if err != nil {
		return nil, fmt.Errorf("raw copy: %w", err)
	}

	var recipientData RecipientData
	if err := json.Unmarshal(rawJSON, &recipientData); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	return &recipientData, nil
}

func (t *ROTxn) GetNextNonce(owner ethcommon.Address) (Nonce, error) {
	data, err := t.GetRecipientData(owner)
	if err != nil {
		return Nonce(0), fmt.Errorf("could not get recipient data: %w", err)
	}

	if data == nil || data.Nonce == nil {
		return Nonce(0), nil
	}

	return *data.Nonce + 1, nil
}
