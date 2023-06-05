package storage

import (
	"bytes"
	"encoding/json"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"

	"github.com/bloxapp/ssv/storage/basedb"
)

var (
	eventsPrefix = []byte("events")
)

// EventData the public data of an event
type EventData struct {
	TxHash common.Hash `json:"txHash"`
}

// Events is the interface for managing events data
type Events interface {
	GetEventData(txHash common.Hash) (*EventData, bool, error)
	SaveEventData(txHash common.Hash) error
	GetEventsPrefix() []byte
}

type eventsStorage struct {
	db     basedb.IDb
	lock   sync.RWMutex
	prefix []byte
}

// NewEventsStorage creates a new instance of Storage
func NewEventsStorage(db basedb.IDb, prefix []byte) Events {
	return &eventsStorage{
		db:     db,
		prefix: prefix,
	}
}

// GetEventsPrefix returns the prefix
func (s *eventsStorage) GetEventsPrefix() []byte {
	return eventsPrefix
}

// GetEventData returns data of the given event by txHash
func (s *eventsStorage) GetEventData(txHash common.Hash) (*EventData, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.getEventData(txHash)
}

func (s *eventsStorage) getEventData(txHash common.Hash) (*EventData, bool, error) {
	obj, found, err := s.db.Get(s.prefix, buildEventKey(txHash))
	if err != nil {
		return nil, found, err
	}
	if !found {
		return nil, found, nil
	}
	var eventData EventData
	err = json.Unmarshal(obj.Value, &eventData)
	return &eventData, found, err
}

// SaveEventData saves event data and return it.
// if the event already exists return nil
func (s *eventsStorage) SaveEventData(txHash common.Hash) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	_, found, err := s.getEventData(txHash)
	if err != nil {
		return errors.Wrap(err, "could not get event data")
	}
	if found {
		return nil
	}

	raw, err := json.Marshal(&EventData{
		TxHash: txHash,
	})
	if err != nil {
		return errors.Wrap(err, "could not marshal event data")
	}
	return s.db.Set(s.prefix, buildEventKey(txHash), raw)
}

// buildEventKey builds event key using eventsPrefix & txHash, e.g. "events/0x00..01"
func buildEventKey(txHash common.Hash) []byte {
	return bytes.Join([][]byte{eventsPrefix, txHash.Bytes()}, []byte("/"))
}
