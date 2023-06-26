package eventdb

import (
	"math/big"

	"github.com/dgraph-io/badger/v4"
)

var (
	storagePrefix         = []byte("operator/")
	lastProcessedBlockKey = []byte("syncOffset") // TODO: left as syncOffset for compatibility, consider renaming
)

type EventDB struct {
	db *badger.DB
}

func NewEventDB(db *badger.DB) *EventDB {
	return &EventDB{db: db}
}

type RO interface {
	Discard()
	Commit() error
	GetLastProcessedBlock() (*big.Int, error)
}

func (e *EventDB) ROTxn() RO {
	txn := e.db.NewTransaction(false)
	defer txn.Discard()

	return NewROTxn(txn)
}

type RW interface {
	RO
	SetLastProcessedBlock(block *big.Int) error
}

func (e *EventDB) RWTxn() RW {
	txn := e.db.NewTransaction(true)
	defer txn.Discard()

	return NewRWTxn(txn)
}
