package qbftstorage

import (
	"encoding/binary"
	"encoding/json"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/format"
)

const (
	highestKey         = "highest"
	decidedKey         = "decided"
	currentKey         = "current"
	lastChangeRoundKey = "last_change_round"
)

// ibftStorage struct
// instanceType is what separates different iBFT eth2 duty types (attestation, proposal and aggregation)
type ibftStorage struct {
	prefix []byte
	db     basedb.IDb
	logger *zap.Logger
}

// NewQBFTStore create new ibft storage
func NewQBFTStore(db basedb.IDb, logger *zap.Logger, instanceType string) QBFTStore {
	ibft := &ibftStorage{
		prefix: []byte(instanceType),
		db:     db,
		logger: logger,
	}
	return ibft
}

// GetLastDecided gets a signed message for an ibft instance which is the highest
func (i *ibftStorage) GetLastDecided(identifier message.Identifier) (*message.SignedMessage, error) {
	val, found, err := i.get(highestKey, identifier)
	if !found {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	ret := &message.SignedMessage{}
	if err := json.Unmarshal(val, ret); err != nil {
		return nil, errors.Wrap(err, "un-marshaling error")
	}
	return ret, nil
}

// SaveLastDecided saves a signed message for an ibft instance which is currently highest
func (i *ibftStorage) SaveLastDecided(signedMsgs ...*message.SignedMessage) error {
	for _, signedMsg := range signedMsgs {
		value, err := json.Marshal(signedMsg)
		if err != nil {
			return errors.Wrap(err, "marshaling error")
		}
		if err = i.save(value, highestKey, signedMsg.Message.Identifier); err != nil {
			return err
		}
	}

	return nil
}

func (i *ibftStorage) GetDecided(identifier message.Identifier, from message.Height, to message.Height) ([]*message.SignedMessage, error) {
	prefix := make([]byte, len(i.prefix))
	copy(prefix, i.prefix)
	prefix = append(prefix, identifier...)

	var sequences [][]byte
	for seq := from; seq <= to; seq++ {
		sequences = append(sequences, i.key(decidedKey, uInt64ToByteSlice(uint64(seq))))
	}
	msgs := make([]*message.SignedMessage, 0)
	err := i.db.GetMany(prefix, sequences, func(obj basedb.Obj) error {
		msg := message.SignedMessage{}
		if err := json.Unmarshal(obj.Value, &msg); err != nil {
			return errors.Wrap(err, "un-marshaling error")
		}
		msgs = append(msgs, &msg)
		return nil
	})
	if err != nil {
		return []*message.SignedMessage{}, err
	}
	return msgs, nil
}

func (i *ibftStorage) SaveDecided(signedMsg ...*message.SignedMessage) error {
	return i.db.SetMany(i.prefix, len(signedMsg), func(j int) (basedb.Obj, error) {
		msg := signedMsg[j]
		k := i.key(decidedKey, uInt64ToByteSlice(uint64(msg.Message.Height)))
		key := append(msg.Message.Identifier, k...)
		value, err := json.Marshal(msg)
		if err != nil {
			return basedb.Obj{}, err
		}
		return basedb.Obj{Key: key, Value: value}, nil
	})
}

func (i *ibftStorage) SaveCurrentInstance(identifier message.Identifier, state *qbft.State) error {
	value, err := json.Marshal(state)
	if err != nil {
		return errors.Wrap(err, "marshaling error")
	}
	return i.save(value, currentKey, identifier)
}

func (i *ibftStorage) GetCurrentInstance(identifier message.Identifier) (*qbft.State, bool, error) {
	val, found, err := i.get(currentKey, identifier)
	if !found {
		return nil, found, nil
	}
	if err != nil {
		return nil, false, err
	}
	ret := &qbft.State{}
	if err := json.Unmarshal(val, ret); err != nil {
		return nil, false, errors.Wrap(err, "un-marshaling error")
	}
	return ret, false, nil
}

// SaveLastChangeRoundMsg updates last change round message
// TODO
func (i *ibftStorage) SaveLastChangeRoundMsg(msg *message.SignedMessage) error {
	value, err := json.Marshal(msg)
	if err != nil {
		return errors.Wrap(err, "marshaling error")
	}
	return i.save(value, lastChangeRoundKey, msg.Message.Identifier)
}

// GetLastChangeRoundMsg returns last known change round message
// TODO
func (i *ibftStorage) GetLastChangeRoundMsg(identifier message.Identifier) (*message.SignedMessage, error) {
	val, found, err := i.get(lastChangeRoundKey, identifier)
	if !found {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ret := &message.SignedMessage{}
	if err := json.Unmarshal(val, ret); err != nil {
		return nil, errors.Wrap(err, "un-marshaling error")
	}
	return ret, nil
}

func (i *ibftStorage) CleanLastChangeRound(identifier message.Identifier) {
	// use v1 identifier, if not found use the v0. this is to support old msg types when sync history
	err := i.delete(lastChangeRoundKey, identifier)
	if err != nil {
		i.logger.Warn("could not clean last change round message", zap.Error(err))
	}
	// doing the same for v0
	oldIdentifier := []byte(format.IdentifierFormat(identifier.GetValidatorPK(), identifier.GetRoleType().String()))
	err = i.delete(lastChangeRoundKey, oldIdentifier)
	if err != nil {
		i.logger.Warn("could not clean last change round message", zap.Error(err))
	}
}

func (i *ibftStorage) save(value []byte, id string, pk []byte, keyParams ...[]byte) error {
	prefix := append(i.prefix, pk...)
	key := i.key(id, keyParams...)
	return i.db.Set(prefix, key, value)
}

func (i *ibftStorage) get(id string, pk []byte, keyParams ...[]byte) ([]byte, bool, error) {
	prefix := append(i.prefix, pk...)
	key := i.key(id, keyParams...)
	obj, found, err := i.db.Get(prefix, key)
	if !found {
		return nil, found, nil
	}
	if err != nil {
		return nil, found, err
	}
	return obj.Value, found, nil
}

func (i *ibftStorage) delete(id string, pk []byte, keyParams ...[]byte) error {
	prefix := append(i.prefix, pk...)
	key := i.key(id, keyParams...)
	return i.db.Delete(prefix, key)
}

func (i *ibftStorage) key(id string, params ...[]byte) []byte {
	ret := []byte(id)
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
