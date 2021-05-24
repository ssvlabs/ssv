package collections

import (
	"encoding/binary"
	"encoding/json"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/storage"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

const (
	// EntryNotFoundError is an error for a storage entry not found
	EntryNotFoundError = "EntryNotFoundError"
)

// Iibft is an interface for persisting chain data
type Iibft interface {
	// SaveCurrentInstance saves the state for the current running (not yet decided) instance
	SaveCurrentInstance(state *proto.State) error
	// GetCurrentInstance returns the state for the current running (not yet decided) instance
	GetCurrentInstance(pk []byte) (*proto.State, error)
	// SaveDecided saves a signed message for an ibft instance with decided justification
	SaveDecided(signedMsg *proto.SignedMessage) error
	// GetDecided returns a signed message for an ibft instance which decided by identifier
	GetDecided(pk []byte, seqNumber uint64) (*proto.SignedMessage, error)
	// SaveHighestDecidedInstance saves a signed message for an ibft instance which is currently highest
	SaveHighestDecidedInstance(signedMsg *proto.SignedMessage) error
	// GetHighestDecidedInstance gets a signed message for an ibft instance which is the highest
	GetHighestDecidedInstance(pk []byte) (*proto.SignedMessage, error)
}

// IbftStorage struct
// instanceType is what separates different iBFT eth2 duty types (attestation, proposal and aggregation)
type IbftStorage struct {
	prefix []byte
	db     storage.IKvStorage
	logger *zap.Logger
}

// NewIbft create new ibft storage
func NewIbft(db storage.IKvStorage, logger *zap.Logger, instanceType string) IbftStorage {
	ibft := IbftStorage{
		prefix: []byte(instanceType),
		db:     db,
		logger: logger,
	}
	return ibft
}

// SaveCurrentInstance func implementation
func (i *IbftStorage) SaveCurrentInstance(state *proto.State) error {
	value, err := json.Marshal(state)
	if err != nil {
		return errors.Wrap(err, "marshaling error")
	}
	return i.save(value, "current", state.ValidatorPk)
}

// GetCurrentInstance func implementation
func (i *IbftStorage) GetCurrentInstance(pk []byte) (*proto.State, error) {
	val, err := i.get("current", pk)
	if err != nil {
		return nil, errors.New(EntryNotFoundError)
	}
	ret := &proto.State{}
	if err := json.Unmarshal(val, ret); err != nil {
		return nil, errors.Wrap(err, "un-marshaling error")
	}
	return ret, nil
}

// SaveDecided func implementation
func (i *IbftStorage) SaveDecided(signedMsg *proto.SignedMessage) error {
	value, err := json.Marshal(signedMsg)
	if err != nil {
		return errors.Wrap(err, "marshaling error")
	}
	return i.save(value, "decided", signedMsg.Message.ValidatorPk, uInt64ToByteSlice(signedMsg.Message.SeqNumber))
}

// GetDecided returns a signed message for an ibft instance which decided by identifier
func (i *IbftStorage) GetDecided(pk []byte, seqNumber uint64) (*proto.SignedMessage, error) {
	val, err := i.get("decided", pk, uInt64ToByteSlice(seqNumber))
	if err != nil {
		return nil, errors.New(EntryNotFoundError)
	}
	ret := &proto.SignedMessage{}
	if err := json.Unmarshal(val, ret); err != nil {
		return nil, errors.Wrap(err, "un-marshaling error")
	}
	return ret, nil
}

// SaveHighestDecidedInstance saves a signed message for an ibft instance which is currently highest
func (i *IbftStorage) SaveHighestDecidedInstance(signedMsg *proto.SignedMessage) error {
	value, err := json.Marshal(signedMsg)
	if err != nil {
		return errors.Wrap(err, "marshaling error")
	}
	return i.save(value, "highest", signedMsg.Message.ValidatorPk)
}

// GetHighestDecidedInstance gets a signed message for an ibft instance which is the highest
func (i *IbftStorage) GetHighestDecidedInstance(pk []byte) (*proto.SignedMessage, error) {
	val, err := i.get("highest", pk)
	if err != nil {
		return nil, errors.New(EntryNotFoundError)
	}
	ret := &proto.SignedMessage{}
	if err := json.Unmarshal(val, ret); err != nil {
		return nil, errors.Wrap(err, "un-marshaling error")
	}
	return ret, nil
}

func (i *IbftStorage) save(value []byte, id string, pk []byte, keyParams ...[]byte) error {
	prefix := append(i.prefix, pk...)
	key := i.key(id, keyParams...)
	return i.db.Set(prefix, key, value)
}

func (i *IbftStorage) get(id string, pk []byte, keyParams ...[]byte) ([]byte, error) {
	prefix := append(i.prefix, pk...)
	key := i.key(id, keyParams...)
	obj, err := i.db.Get(prefix, key)
	if err != nil {
		return nil, err
	}
	return obj.Value, nil
}

func (i *IbftStorage) key(id string, params ...[]byte) []byte {
	ret := make([]byte, 0)
	ret = append(ret, []byte(id)...)
	for _, p := range params {
		ret = append(ret, p...)
	}
	return ret
}

func uInt64ToByteSlice(n uint64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, n)
	return b
}
