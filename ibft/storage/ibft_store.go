package storage

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	v0 "github.com/bloxapp/ssv/ibft/conversion"
	"log"

	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/format"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

const (
	highestKey         = "highest"
	decidedKey         = "decided"
	currentKey         = "current"
	lastChangeRoundKey = "last_change_round"
)

var (
	metricsHighestDecided = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:validator:ibft_highest_decided",
		Help: "The highest decided sequence number",
	}, []string{"lambda", "pubKey"})
)

func init() {
	if err := prometheus.Register(metricsHighestDecided); err != nil {
		log.Println("could not register prometheus collector")
	}
}

// ibftStorage struct
// instanceType is what separates different iBFT eth2 duty types (attestation, proposal and aggregation)
type ibftStorage struct {
	prefix []byte
	db     basedb.IDb
	logger *zap.Logger
}

// New create new ibft storage
func New(db basedb.IDb, logger *zap.Logger, prefix string) qbftstorage.QBFTStore {
	ibft := &ibftStorage{
		prefix: []byte(prefix),
		db:     db,
		logger: logger,
	}
	return ibft
}

// GetLastDecided gets a signed message for an ibft instance which is the highest
func (i *ibftStorage) GetLastDecided(identifier message.Identifier) (*message.SignedMessage, error) {
	// use the v1 identifier, if not found use the v0. this is to support old msg types when sync history
	val, found, err := i.get(highestKey, identifier)
	if found && err == nil {
		// old one not found, just unmarshal v1
		ret := &message.SignedMessage{}
		if err := json.Unmarshal(val, ret); err == nil { // if err, just continue to v0
			return ret, nil
		}
	}

	// v1 not found, try with v0 identifier
	oldIdentifier := format.IdentifierFormat(identifier.GetValidatorPK(), identifier.GetRoleType().String())
	val, found, err = i.get(highestKey, []byte(oldIdentifier))
	if !found {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// old val found, unmarshal with old struct and convert to v1
	ret := &proto.SignedMessage{}
	if err := json.Unmarshal(val, ret); err != nil {
		return nil, errors.Wrap(err, "un-marshaling error")
	}
	return v0.ToSignedMessageV1(ret)
}

// SaveLastDecided saves a signed message for an ibft instance which is currently highest
func (i *ibftStorage) SaveLastDecided(signedMsgs ...*message.SignedMessage) error {
	for _, signedMsg := range signedMsgs {
		value, err := signedMsg.Encode()
		if err != nil {
			return errors.Wrap(err, "marshaling error")
		}
		if err = i.save(value, highestKey, signedMsg.Message.Identifier); err != nil {
			return err
		}
		reportHighestDecided(signedMsg)
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

	// use the v1 identifier, if not found use the v0. this is to support old msg types when sync history
	msgs := make([]*message.SignedMessage, 0)
	err := i.db.GetMany(prefix, sequences, func(obj basedb.Obj) error {
		msg := message.SignedMessage{}
		if err := json.Unmarshal(obj.Value, &msg); err != nil {
			return errors.Wrap(err, "un-marshaling error")
		}
		msgs = append(msgs, &msg)
		return nil
	})
	if err == nil && len(msgs) > 0 {
		return msgs, nil
	}

	// v1 not found, get v0 identifier and unmarshal to v1
	oldPrefix := make([]byte, len(i.prefix))
	copy(oldPrefix, i.prefix)
	oldIdentifier := []byte(format.IdentifierFormat(identifier.GetValidatorPK(), identifier.GetRoleType().String()))
	oldPrefix = append(oldPrefix, oldIdentifier...)

	msgs = make([]*message.SignedMessage, 0)
	err = i.db.GetMany(oldPrefix, sequences, func(obj basedb.Obj) error {
		ret := proto.SignedMessage{}
		if err := json.Unmarshal(obj.Value, &ret); err != nil {
			return errors.Wrap(err, "un-marshaling error")
		}
		msg, err := v0.ToSignedMessageV1(&ret)
		if err != nil {
			return err
		}
		msgs = append(msgs, msg)
		return nil
	})
	return msgs, err
}

func (i *ibftStorage) SaveDecided(signedMsg ...*message.SignedMessage) error {
	return i.db.SetMany(i.prefix, len(signedMsg), func(j int) (basedb.Obj, error) {
		msg := signedMsg[j]
		k := i.key(decidedKey, uInt64ToByteSlice(uint64(msg.Message.Height)))
		key := append(msg.Message.Identifier, k...)
		value, err := msg.Encode()
		if err != nil {
			return basedb.Obj{}, err
		}
		return basedb.Obj{Key: key, Value: value}, nil
	})
}

func (i *ibftStorage) SaveCurrentInstance(identifier message.Identifier, state *qbft.State) error {
	value, err := state.MarshalJSON()
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
	if err := ret.UnmarshalJSON(val); err != nil {
		return nil, false, errors.Wrap(err, "un-marshaling error")
	}
	return ret, found, nil
}

// SaveLastChangeRoundMsg updates last change round message
func (i *ibftStorage) SaveLastChangeRoundMsg(identifier message.Identifier, msg *message.SignedMessage) error {
	value, err := msg.Encode()
	if err != nil {
		return errors.Wrap(err, "marshaling error")
	}
	return i.save(value, lastChangeRoundKey, identifier)
}

// GetLastChangeRoundMsg returns last known change round message
func (i *ibftStorage) GetLastChangeRoundMsg(identifier message.Identifier) (*message.SignedMessage, error) {
	// use v1 identifier, if not found use the v0. this is to support old msg types when sync history
	val, found, err := i.get(lastChangeRoundKey, identifier)
	if found && err == nil {
		ret := &message.SignedMessage{}
		if err := ret.Decode(val); err == nil {
			return ret, nil
		}
	}

	// v1 not found, try with v0 identifier
	oldIdentifier := []byte(format.IdentifierFormat(identifier.GetValidatorPK(), identifier.GetRoleType().String()))
	val, found, err = i.get(lastChangeRoundKey, oldIdentifier)
	if !found {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// old val found, unmarshal with old struct and convert to v1
	ret := &proto.SignedMessage{}
	if err := json.Unmarshal(val, ret); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal signed message")
	}
	return v0.ToSignedMessageV1(ret)
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

func reportHighestDecided(signedMsg *message.SignedMessage) {
	pk := hex.EncodeToString(signedMsg.Message.Identifier.GetValidatorPK())
	metricsHighestDecided.WithLabelValues(signedMsg.Message.Identifier.String(), pk).
		Set(float64(signedMsg.Message.Height))
}
