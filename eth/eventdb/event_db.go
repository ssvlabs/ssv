package eventdb

import (
	"math/big"

	"github.com/dgraph-io/badger/v4"
	ethcommon "github.com/ethereum/go-ethereum/common"

	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
)

var (
	storagePrefix         = "operator/"
	operatorsPrefix       = "operators"
	recipientsPrefix      = "recipients"
	sharesPrefix          = "shares"
	lastProcessedBlockKey = "syncOffset" // TODO: temporarily left as syncOffset for compatibility, consider renaming
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
	GetOperatorData(id uint64) (*registrystorage.OperatorData, error)
	GetRecipientData(owner ethcommon.Address) (*registrystorage.RecipientData, error)
	GetNextNonce(owner ethcommon.Address) (registrystorage.Nonce, error)
}

func (e *EventDB) ROTxn() RO {
	txn := e.db.NewTransaction(false)
	return NewROTxn(txn)
}

type RW interface {
	RO
	SetLastProcessedBlock(block *big.Int) error
	SaveOperatorData(od *registrystorage.OperatorData) (bool, error)
	BumpNonce(owner ethcommon.Address) error
	SaveRecipientData(recipientData *registrystorage.RecipientData) (*registrystorage.RecipientData, error)
	SaveShares(shares ...*ssvtypes.SSVShare) error
	DeleteShare(pubKey []byte) error
}

func (e *EventDB) RWTxn() RW {
	txn := e.db.NewTransaction(true)
	return NewRWTxn(txn)
}
