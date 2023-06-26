package eventdb

import (
	"fmt"
	"math/big"

	"github.com/dgraph-io/badger/v4"
)

type RWTxn struct {
	ROTxn
}

func NewRWTxn(txn *badger.Txn) *RWTxn {
	return &RWTxn{ROTxn{txn: txn}}
}

func (t *RWTxn) SetLastProcessedBlock(block *big.Int) error {
	if err := t.txn.Set(append(storagePrefix, lastProcessedBlockKey...), block.Bytes()); err != nil {
		return fmt.Errorf("set item: %w", err)
	}

	return nil
}
