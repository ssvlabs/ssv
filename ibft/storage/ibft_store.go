package storage

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	forksv0 "github.com/bloxapp/ssv/ibft/storage/forks/v0"
	"log"
	"sync"

	"github.com/bloxapp/ssv/ibft/conversion"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/storage/forks"
	forksfactory "github.com/bloxapp/ssv/ibft/storage/forks/factory"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
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
	prefix   []byte
	db       basedb.IDb
	logger   *zap.Logger
	fork     forks.Fork
	forkLock *sync.RWMutex
}

// New create new ibft storage
func New(db basedb.IDb, logger *zap.Logger, prefix string, forkVersion forksprotocol.ForkVersion) qbftstorage.QBFTStore {
	ibft := &ibftStorage{
		prefix:   []byte(prefix),
		db:       db,
		logger:   logger,
		fork:     forksfactory.NewFork(forkVersion),
		forkLock: &sync.RWMutex{},
	}
	return ibft
}

func (i *ibftStorage) OnFork(forkVersion forksprotocol.ForkVersion) error {
	i.forkLock.Lock()
	defer i.forkLock.Unlock()

	logger := i.logger.With(zap.String("where", "OnFork"))
	logger.Info("forking ibft storage")
	i.fork = forksfactory.NewFork(forkVersion)
	return nil
}

// GetLastDecided gets a signed message for an ibft instance which is the highest
// it tries to read current fork items, and if not found it tries to read v0 items
func (i *ibftStorage) GetLastDecided(identifier message.Identifier) (*message.SignedMessage, error) {
	i.forkLock.RLock()
	defer i.forkLock.RUnlock()

	val, found, err := i.get(highestKey, i.fork.Identifier(identifier.GetValidatorPK(), identifier.GetRoleType()))
	if err != nil {
		return nil, err
	}
	if !found {
		// trying v0 if v1 not found
		identifierV0 := []byte(format.IdentifierFormat(identifier.GetValidatorPK(), identifier.GetRoleType().String()))
		val, found, err = i.get(highestKey, identifierV0)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, nil
		}
		return forksv0.ForkV0{}.DecodeSignedMsg(val)
	}
	return i.fork.DecodeSignedMsg(val)
}

// SaveLastDecided saves a signed message for an ibft instance which is currently highest
func (i *ibftStorage) SaveLastDecided(signedMsgs ...*message.SignedMessage) error {
	i.forkLock.RLock()
	defer i.forkLock.RUnlock()

	for _, signedMsg := range signedMsgs {
		identifier := i.fork.Identifier(signedMsg.Message.Identifier.GetValidatorPK(), signedMsg.Message.Identifier.GetRoleType())
		value, err := i.fork.EncodeSignedMsg(signedMsg)
		if err != nil {
			return errors.Wrap(err, "could not encode signed message")
		}
		if err = i.save(value, highestKey, identifier); err != nil {
			return err
		}
		reportHighestDecided(signedMsg)
	}

	return nil
}

func (i *ibftStorage) GetDecided(identifier message.Identifier, from message.Height, to message.Height) ([]*message.SignedMessage, error) {
	i.forkLock.RLock()
	defer i.forkLock.RUnlock()

	identifierV0 := []byte(format.IdentifierFormat(identifier.GetValidatorPK(), identifier.GetRoleType().String()))
	msgs := make([]*message.SignedMessage, 0)

	for seq := from; seq <= to; seq++ {
		// use the v1 identifier, if not found use the v0. this is to support old msg types when sync history
		val, found, err := i.get(decidedKey, identifier, uInt64ToByteSlice(uint64(seq)))
		if err != nil {
			return msgs, err
		}
		if found {
			msg := message.SignedMessage{}
			if err := json.Unmarshal(val, &msg); err != nil {
				return msgs, errors.Wrap(err, "could not unmarshal signed message v1")
			}
			msgs = append(msgs, &msg)
			continue
		}

		// v1 not found, try with v0 identifier
		val, found, err = i.get(decidedKey, identifierV0, uInt64ToByteSlice(uint64(seq)))
		if err != nil {
			return msgs, err
		}
		if found {
			ret := proto.SignedMessage{}
			if err := json.Unmarshal(val, &ret); err != nil {
				return msgs, errors.Wrap(err, "could not unmarshal signed message v0")
			}
			msg, err := conversion.ToSignedMessageV1(&ret)
			if err != nil {
				return msgs, err
			}
			msgs = append(msgs, msg)
		}
	}

	return msgs, nil
}

func (i *ibftStorage) SaveDecided(signedMsg ...*message.SignedMessage) error {
	i.forkLock.RLock()
	defer i.forkLock.RUnlock()

	return i.db.SetMany(i.prefix, len(signedMsg), func(j int) (basedb.Obj, error) {
		msg := signedMsg[j]
		k := i.key(decidedKey, uInt64ToByteSlice(uint64(msg.Message.Height)))
		value, err := i.fork.EncodeSignedMsg(msg)
		if err != nil {
			return basedb.Obj{}, err
		}
		identifier := i.fork.Identifier(msg.Message.Identifier.GetValidatorPK(), msg.Message.Identifier.GetRoleType())
		key := append(identifier, k...)
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
func (i *ibftStorage) SaveLastChangeRoundMsg(msg *message.SignedMessage) error {
	i.forkLock.RLock()
	defer i.forkLock.RUnlock()

	var signers [][]byte
	for _, s := range msg.GetSigners() {
		signers = append(signers, uInt64ToByteSlice(uint64(s)))
	}

	identifier := i.fork.Identifier(msg.Message.Identifier.GetValidatorPK(), msg.Message.Identifier.GetRoleType())
	signedMsg, err := i.fork.EncodeSignedMsg(msg)
	if err != nil {
		return errors.Wrap(err, "could not encode signed message")
	}
	return i.save(signedMsg, lastChangeRoundKey, identifier, signers...)
}

// GetLastChangeRoundMsg returns last known change round message
func (i *ibftStorage) GetLastChangeRoundMsg(identifier message.Identifier, signers ...message.OperatorID) ([]*message.SignedMessage, error) {
	i.forkLock.RLock()
	defer i.forkLock.RUnlock()

	if len(signers) == 0 {
		res, err := i.getAll(lastChangeRoundKey, i.fork.Identifier(identifier.GetValidatorPK(), identifier.GetRoleType()))

		if err != nil {
			return nil, err
		}
		return res, nil
	}

	var res []*message.SignedMessage
	for _, s := range signers {
		msg, found, err := i.get(lastChangeRoundKey, i.fork.Identifier(identifier.GetValidatorPK(), identifier.GetRoleType()), uInt64ToByteSlice(uint64(s)))
		if err != nil {
			return res, err
		}
		if !found {
			return res, nil
		}
		sm := new(message.SignedMessage)
		if err := sm.Decode(msg); err != nil {
			return res, err
		}
		res = append(res, sm)
	}
	return res, nil
}

// CleanLastChangeRound cleans last change round message of some validator, should be called upon controller init
func (i *ibftStorage) CleanLastChangeRound(identifier message.Identifier) {
	i.forkLock.RLock()
	defer i.forkLock.RUnlock()

	forkIdentifier := i.fork.Identifier(identifier.GetValidatorPK(), identifier.GetRoleType())
	err := i.delete(lastChangeRoundKey, forkIdentifier)
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

func (i *ibftStorage) getAll(id string, pk []byte) ([]*message.SignedMessage, error) {
	prefix := append(i.prefix, pk...)
	prefix = append(prefix, id...)

	var res []*message.SignedMessage
	err := i.db.GetAll(prefix, func(i int, obj basedb.Obj) error {
		msg := new(message.SignedMessage)
		if err := msg.Decode(obj.Value); err != nil {
			return err
		}
		res = append(res, msg)
		return nil
	})

	return res, err
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

func reportHighestDecided(signedMsg *message.SignedMessage) {
	pk := hex.EncodeToString(signedMsg.Message.Identifier.GetValidatorPK())
	metricsHighestDecided.WithLabelValues(signedMsg.Message.Identifier.String(), pk).
		Set(float64(signedMsg.Message.Height))
}
