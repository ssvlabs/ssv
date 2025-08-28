package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/storage/basedb"
)

//go:generate go tool -modfile=../../tool.mod mockgen -package=mocks -destination=./mocks/recipients.go -source=./recipients.go

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
		return fmt.Errorf("invalid JSON: %w", err)
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
	GetFeeRecipient(owner common.Address) (bellatrix.ExecutionAddress, error)
	GetNextNonce(r basedb.Reader, owner common.Address) (Nonce, error)
	BumpNonce(rw basedb.ReadWriter, owner common.Address) error
	SaveRecipientData(rw basedb.ReadWriter, owner common.Address, feeRecipient bellatrix.ExecutionAddress) (*RecipientData, error)
	DeleteRecipientData(rw basedb.ReadWriter, owner common.Address) error
	DropRecipients() error
}

// recipientsStorage implements the Recipients interface for managing fee recipient data.
// It maintains an in-memory cache of all fee recipients for fast access while
// persisting data to the underlying database.
type recipientsStorage struct {
	logger *zap.Logger
	db     basedb.Database
	// lock protects concurrent access to recipient data, ensuring atomic operations
	// across both the in-memory feeRecipients map and database operations (nonce management)
	lock   sync.RWMutex
	prefix []byte

	// In-memory map of all fee recipients for fast access
	feeRecipients map[common.Address]bellatrix.ExecutionAddress
}

// NewRecipientsStorage creates a new instance of Storage.
// The returned *recipientsStorage implements the Recipients interface.
func NewRecipientsStorage(logger *zap.Logger, db basedb.Database, prefix []byte) (*recipientsStorage, error) {
	rs := &recipientsStorage{
		logger:        logger,
		db:            db,
		prefix:        prefix,
		feeRecipients: make(map[common.Address]bellatrix.ExecutionAddress),
	}

	// Load all recipients into memory
	if err := rs.loadFromDB(); err != nil {
		return nil, fmt.Errorf("could not load recipients from database: %w", err)
	}

	return rs, nil
}

// loadFromDB loads all recipients from database into memory map
func (s *recipientsStorage) loadFromDB() error {
	return s.db.GetAll(RecipientsDBPrefix(s.prefix), func(i int, obj basedb.Obj) error {
		// Extract owner address from the key
		// Key format after prefix: just the 20-byte owner address
		if len(obj.Key) != bellatrix.ExecutionAddressLength {
			return fmt.Errorf("invalid owner address length: %d", len(obj.Key))
		}

		var recipientData RecipientData
		if err := json.Unmarshal(obj.Value, &recipientData); err != nil {
			return fmt.Errorf("failed to decode recipient data: %w", err)
		}

		// Use the owner from the decoded data for consistency
		s.feeRecipients[recipientData.Owner] = recipientData.FeeRecipient
		return nil
	})
}

// GetFeeRecipient returns the fee recipient for a specific owner.
// IMPORTANT: This method returns an error if no fee recipient is configured.
// It does NOT automatically fall back to the owner address.
// The system design ensures that every active validator has a fee recipient configured
// through BumpNonce or SaveRecipientData before this method is called.
func (s *recipientsStorage) GetFeeRecipient(owner common.Address) (bellatrix.ExecutionAddress, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	recipient, found := s.feeRecipients[owner]
	if !found {
		return bellatrix.ExecutionAddress{}, fmt.Errorf("fee recipient not found for owner %s", owner.Hex())
	}
	return recipient, nil
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

func (s *recipientsStorage) GetNextNonce(r basedb.Reader, owner common.Address) (Nonce, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	data, found, err := s.getRecipientData(r, owner)
	if err != nil {
		return Nonce(0), fmt.Errorf("could not get recipient data: %w", err)
	}
	if !found {
		return Nonce(0), nil
	}
	if data == nil {
		return Nonce(0), fmt.Errorf("recipient data is nil")
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
		return fmt.Errorf("could not get recipient data: %w", err)
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
		return fmt.Errorf("recipient data is nil")
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
		return fmt.Errorf("could not marshal recipient data: %w", err)
	}

	if err = s.db.Using(rw).Set(s.prefix, buildRecipientKey(rData.Owner), raw); err != nil {
		return fmt.Errorf("could not set recipient data: %w", err)
	}

	// Update in-memory map only if this is a new recipient
	// This handles the case where a validator is added before fee recipient is set
	if !found {
		s.feeRecipients[owner] = rData.FeeRecipient
	}

	return nil
}

// SaveRecipientData saves or updates fee recipient for an owner
// Returns the saved data, or nil if the fee recipient didn't change
func (s *recipientsStorage) SaveRecipientData(rw basedb.ReadWriter, owner common.Address, feeRecipient bellatrix.ExecutionAddress) (*RecipientData, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	// Check if recipient data exists
	existingData, found, err := s.getRecipientData(rw, owner)
	if err != nil {
		return nil, fmt.Errorf("could not get recipient data: %w", err)
	}

	// If fee recipient hasn't changed, return nil
	if found && existingData.FeeRecipient == feeRecipient {
		return nil, nil
	}

	// Prepare recipient data
	recipientData := existingData
	if !found {
		recipientData = &RecipientData{
			Owner: owner,
		}
	}
	recipientData.FeeRecipient = feeRecipient

	// Marshal and save
	raw, err := json.Marshal(recipientData)
	if err != nil {
		return nil, fmt.Errorf("could not marshal recipient data: %w", err)
	}

	if err = s.db.Using(rw).Set(s.prefix, buildRecipientKey(owner), raw); err != nil {
		return nil, fmt.Errorf("could not set recipient data: %w", err)
	}

	// Update in-memory map
	s.feeRecipients[owner] = feeRecipient

	return recipientData, nil
}

func (s *recipientsStorage) DeleteRecipientData(rw basedb.ReadWriter, owner common.Address) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if err := s.db.Using(rw).Delete(s.prefix, buildRecipientKey(owner)); err != nil {
		return fmt.Errorf("could not delete recipient data: %w", err)
	}

	// Remove from in-memory map
	delete(s.feeRecipients, owner)

	return nil
}

func (s *recipientsStorage) DropRecipients() error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if err := s.db.DropPrefix(RecipientsDBPrefix(s.prefix)); err != nil {
		return fmt.Errorf("could not drop recipients prefix: %w", err)
	}

	// Clear in-memory map
	s.feeRecipients = make(map[common.Address]bellatrix.ExecutionAddress)

	return nil
}

// RecipientsDBPrefix builds a DB prefix all recipient keys are stored under.
func RecipientsDBPrefix(storagePrefix []byte) []byte {
	return bytes.Join([][]byte{storagePrefix, recipientsPrefix, []byte("/")}, nil)
}

// buildRecipientKey builds recipient key using recipientsPrefix & owner address, e.g. "recipients/0x00..01"
func buildRecipientKey(owner common.Address) []byte {
	return bytes.Join([][]byte{recipientsPrefix, owner.Bytes()}, []byte("/"))
}
