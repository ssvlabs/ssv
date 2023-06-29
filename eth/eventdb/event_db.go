package eventdb

import (
	"math/big"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/dgraph-io/badger/v4"
	ethcommon "github.com/ethereum/go-ethereum/common"

	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
)

var (
	storagePrefix         = "operator/"
	operatorsPrefix       = "operators"
	eventsPrefix          = "events"
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
	GetRecipientData(owner ethcommon.Address) (*RecipientData, error)
	GetNextNonce(owner ethcommon.Address) (Nonce, error)
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
	SaveRecipientData(recipientData *RecipientData) (*RecipientData, error)
	SaveShares(shares ...*ssvtypes.SSVShare) error
	DeleteShare(pubKey []byte) error
}

func (e *EventDB) RWTxn() RW {
	txn := e.db.NewTransaction(true)
	return NewRWTxn(txn)
}

// TODO: get rid of duplication

type Nonce uint16

// RecipientData the public data of a recipient
type RecipientData struct {
	Owner        ethcommon.Address          `json:"ownerAddress"`
	FeeRecipient bellatrix.ExecutionAddress `json:"feeRecipientAddress"`

	// Nonce: This field represents the 'ValidatorAdded Event' nonce.
	// It serves a crucial role in protecting against replay attacks.
	// Each time a new validator added event is triggered, regardless of whether the event is malformed or not,
	// we increment this nonce by 1.
	// ** The Nonce field can be nil because the 'FeeRecipientAddressUpdatedEvent'
	// might occur before the addition of a validator to the network, and this event does not increment the nonce.
	Nonce *Nonce `json:"nonce"`
}
