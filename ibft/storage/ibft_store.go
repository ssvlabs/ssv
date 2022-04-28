package storage

import (
	"encoding/binary"
	"encoding/json"
	"github.com/bloxapp/ssv/ibft/proto"
	v0 "github.com/bloxapp/ssv/network/forks/v0"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/format"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
	"log"
	"strings"
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
	// use the old identifier, if not found use the new one. this is to support old msg types when sync history
	oldIdentifier := format.IdentifierFormat(identifier.GetValidatorPK(), identifier.GetRoleType().String())
	val, found, err := i.get(highestKey, []byte(oldIdentifier))
	if found && err == nil {
		// old val found, unmarshal with old struct and convert to v1
		ret := &proto.SignedMessage{}
		if err := json.Unmarshal(val, ret); err != nil {
			return nil, errors.Wrap(err, "un-marshaling error")
		}
		return v0.ToSignedMessageV1(ret)
	}

	// old not found, try with new identifier
	val, found, err = i.get(highestKey, identifier)
	if !found {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// old one not found, just unmarshal v1
	ret := &message.SignedMessage{}
	if err := json.Unmarshal(val, ret); err != nil {
		return nil, errors.Wrap(err, "un-marshaling error")
	}
	return ret, nil
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

	oldPrefix := make([]byte, len(i.prefix))
	copy(oldPrefix, i.prefix)
	oldIdentifier := []byte(format.IdentifierFormat(identifier.GetValidatorPK(), identifier.GetRoleType().String()))
	oldPrefix = append(oldPrefix, oldIdentifier...)

	var sequences [][]byte
	for seq := from; seq <= to; seq++ {
		sequences = append(sequences, i.key(decidedKey, uInt64ToByteSlice(uint64(seq))))
	}
	msgs := make([]*message.SignedMessage, 0)
	err := i.db.GetMany(oldPrefix, sequences, func(obj basedb.Obj) error {
		// old val found, unmarshal with old struct and convert to v1
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

	if err == nil && len(msgs) > 0 {
		// old struct found
		return msgs, nil
	}

	msgs = make([]*message.SignedMessage, 0)
	// old one not found, get new identifier and unmarshal v1
	err = i.db.GetMany(prefix, sequences, func(obj basedb.Obj) error {
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
		value, err := msg.Encode()
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
func (i *ibftStorage) SaveLastChangeRoundMsg(identifier message.Identifier, msg *message.SignedMessage) error {
	value, err := json.Marshal(msg)
	if err != nil {
		return errors.Wrap(err, "marshaling error")
	}
	return i.save(value, lastChangeRoundKey, identifier)
}

// GetLastChangeRoundMsg returns last known change round message
func (i *ibftStorage) GetLastChangeRoundMsg(identifier message.Identifier) (*message.SignedMessage, error) {
	// use the old identifier, if not found use the new one. this is to support old msg types when sync history
	oldIdentifier := []byte(format.IdentifierFormat(identifier.GetValidatorPK(), identifier.GetRoleType().String()))
	val, found, err := i.get(lastChangeRoundKey, oldIdentifier)
	if found && err == nil {
		// old val found, unmarshal with old struct and convert to v1
		ret := &proto.SignedMessage{}
		if err := json.Unmarshal(val, ret); err != nil {
			return nil, errors.Wrap(err, "un-marshaling error")
		}
		return v0.ToSignedMessageV1(ret)
	}

	// old not found, try with new identifier
	val, found, err = i.get(lastChangeRoundKey, identifier)
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
	l := string(signedMsg.Message.Identifier)
	// in order to extract the public key, the role (e.g. '_ATTESTER') is removed
	if idx := strings.Index(l, "_"); idx > 0 {
		pubKey := l[:idx]
		metricsHighestDecided.WithLabelValues(string(signedMsg.Message.Identifier), pubKey).
			Set(float64(signedMsg.Message.Height))
	}
}
