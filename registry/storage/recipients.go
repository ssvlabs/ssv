package storage

import (
	"bytes"
	"encoding/json"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/storage/basedb"
)

var (
	recipientsPrefix = []byte("recipients")
)

type Nonce uint16

// RecipientData the public data of a recipient
type RecipientData struct {
	Owner        common.Address             `json:"ownerAddress"`
	FeeRecipient bellatrix.ExecutionAddress `json:"feeRecipientAddress"`

	// Nonce: This field represents the 'ValidatorAdded Event' nonce.
	// It serves a crucial role in protecting against replay attacks.
	// Each time a new validator added event is triggered, regardless of whether the event is malformed or not,
	// we increment this nonce by 1.
	// ** The Nonce field can be nil because the 'FeeRecipientAddressUpdatedEvent'
	// might occur before the addition of a validator to the network, and this event does not increment the nonce.
	Nonce *Nonce `json:"nonce"`
}

func (r *RecipientData) MarshalJSON() ([]byte, error) {
	return json.Marshal(recipientDataJSON{
		Owner:        r.Owner,
		FeeRecipient: r.FeeRecipient,
		Nonce:        r.Nonce,
	})
}

func (r *RecipientData) UnmarshalJSON(input []byte) error {
	var data recipientDataJSON
	if err := json.Unmarshal(input, &data); err != nil {
		return errors.Wrap(err, "invalid JSON")
	}
	r.Owner = data.Owner
	r.FeeRecipient = data.FeeRecipient
	r.Nonce = data.Nonce
	return nil
}

type recipientDataJSON struct {
	Owner        common.Address `json:"ownerAddress"`
	FeeRecipient [20]byte       `json:"feeRecipientAddress"`
	Nonce        *Nonce         `json:"nonce"`
}

// Recipients is the interface for managing recipients data
type Recipients interface {
	GetRecipientData(r basedb.Reader, owner common.Address) (*RecipientData, bool, error)
	GetRecipientDataMany(r basedb.Reader, owners []common.Address) (map[common.Address]bellatrix.ExecutionAddress, error)
	GetNextNonce(r basedb.Reader, owner common.Address) (Nonce, error)
	BumpNonce(rw basedb.ReadWriter, owner common.Address) error
	SaveRecipientData(rw basedb.ReadWriter, recipientData *RecipientData) (*RecipientData, error)
	DeleteRecipientData(rw basedb.ReadWriter, owner common.Address) error
	DropRecipients() error
	GetRecipientsPrefix() []byte
}

type recipientsStorage struct {
	logger *zap.Logger
	db     basedb.Database
	lock   sync.RWMutex
	prefix []byte
}

// NewRecipientsStorage creates a new instance of Storage
func NewRecipientsStorage(logger *zap.Logger, db basedb.Database, prefix []byte) Recipients {
	return &recipientsStorage{
		logger: logger,
		db:     db,
		prefix: prefix,
	}
}

// GetRecipientsPrefix returns DB prefix
func (s *recipientsStorage) GetRecipientsPrefix() []byte {
	return recipientsPrefix
}

// GetRecipientData returns data of the given recipient by owner address, if not found returns owner address as a default fee recipient
func (s *recipientsStorage) GetRecipientData(r basedb.Reader, owner common.Address) (*RecipientData, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.getRecipientData(r, owner)
}

func (s *recipientsStorage) getRecipientData(r basedb.Reader, owner common.Address) (*RecipientData, bool, error) {
	obj, found, err := s.db.UsingReader(r).Get(s.prefix, buildRecipientKey(owner))
	if err != nil {
		return nil, found, err
	}
	if !found {
		return nil, found, nil
	}

	var recipientData RecipientData
	err = json.Unmarshal(obj.Value, &recipientData)
	return &recipientData, found, err
}

func (s *recipientsStorage) GetRecipientDataMany(r basedb.Reader, owners []common.Address) (map[common.Address]bellatrix.ExecutionAddress, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var keys [][]byte
	for _, owner := range owners {
		keys = append(keys, buildRecipientKey(owner))
	}
	results := make(map[common.Address]bellatrix.ExecutionAddress)
	err := s.db.UsingReader(r).GetMany(s.prefix, keys, func(obj basedb.Obj) error {
		var recipient RecipientData
		err := json.Unmarshal(obj.Value, &recipient)
		if err != nil {
			return errors.Wrap(err, "could not unmarshal recipient data")
		}
		results[recipient.Owner] = recipient.FeeRecipient
		return nil
	})
	if err != nil {
		return nil, err
	}

	return results, nil
}

func (s *recipientsStorage) GetNextNonce(r basedb.Reader, owner common.Address) (Nonce, error) {
	data, found, err := s.GetRecipientData(r, owner)
	if err != nil {
		return Nonce(0), errors.Wrap(err, "could not get recipient data")
	}
	if !found {
		return Nonce(0), nil
	}
	if data == nil {
		return Nonce(0), errors.New("recipient data is nil")
	}
	if data.Nonce == nil {
		return Nonce(0), nil
	}

	return *data.Nonce + 1, nil
}

func (s *recipientsStorage) BumpNonce(rw basedb.ReadWriter, owner common.Address) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	rData, found, err := s.getRecipientData(rw, owner)
	if err != nil {
		return errors.Wrap(err, "could not get recipient data")
	}

	if !found {
		// Create a variable of type Nonce
		nonce := Nonce(0)

		// Create an instance of RecipientData and assign the Nonce and Owner address values
		rData = &RecipientData{
			Owner: owner,
			Nonce: &nonce, // Assign the address of nonceValue to Nonce field
		}
		copy(rData.FeeRecipient[:], owner.Bytes())
	}

	if rData == nil {
		return errors.New("recipient data is nil")
	}

	if rData.Nonce == nil {
		nonce := Nonce(0)
		rData.Nonce = &nonce
	} else if found {
		// Bump the nonce
		*rData.Nonce++
	}

	raw, err := json.Marshal(rData)
	if err != nil {
		return errors.Wrap(err, "could not marshal recipient data")
	}

	return s.db.Using(rw).Set(s.prefix, buildRecipientKey(rData.Owner), raw)
}

// SaveRecipientData saves recipient data and return it.
// if the recipient already exists and the fee didn't change return nil
func (s *recipientsStorage) SaveRecipientData(rw basedb.ReadWriter, recipientData *RecipientData) (*RecipientData, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	r, found, err := s.getRecipientData(rw, recipientData.Owner)
	if err != nil {
		return nil, errors.Wrap(err, "could not get recipient data")
	}
	// same fee recipient
	if found && r.FeeRecipient == recipientData.FeeRecipient {
		return nil, nil
	}

	raw, err := json.Marshal(recipientData)
	if err != nil {
		return nil, errors.Wrap(err, "could not marshal recipient data")
	}

	return recipientData, s.db.Using(rw).Set(s.prefix, buildRecipientKey(recipientData.Owner), raw)
}

func (s *recipientsStorage) DeleteRecipientData(rw basedb.ReadWriter, owner common.Address) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.db.Using(rw).Delete(s.prefix, buildRecipientKey(owner))
}

func (s *recipientsStorage) DropRecipients() error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.db.DropPrefix(bytes.Join(
		[][]byte{s.prefix, recipientsPrefix, []byte("/")},
		nil,
	))
}

// buildRecipientKey builds recipient key using recipientsPrefix & owner address, e.g. "recipients/0x00..01"
func buildRecipientKey(owner common.Address) []byte {
	return bytes.Join([][]byte{recipientsPrefix, owner.Bytes()}, []byte("/"))
}
