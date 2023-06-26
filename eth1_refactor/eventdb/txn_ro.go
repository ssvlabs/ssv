package eventdb

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/dgraph-io/badger/v4"
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
	blockItem, err := t.txn.Get(append(storagePrefix, lastProcessedBlockKey...))
	if err != nil {
		return nil, fmt.Errorf("get item: %w", err)
	}

	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil, nil
	}

	blockValue, err := blockItem.ValueCopy(nil)
	if err != nil {
		return nil, fmt.Errorf("copy value: %w", err)
	}

	return new(big.Int).SetBytes(blockValue), nil
}
