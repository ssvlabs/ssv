package storage

import (
	"encoding/binary"
	"encoding/hex"
	"log"
	"sync"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/storage/forks"
	forksfactory "github.com/bloxapp/ssv/ibft/storage/forks/factory"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	qbftstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	"github.com/bloxapp/ssv/storage/basedb"
)

const (
	highestInstanceKey = "highest_instance"
	instanceKey        = "instance"
	lastChangeRoundKey = "last_change_round"
)

var (
	metricsHighestDecided = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:validator:ibft_highest_decided",
		Help: "The highest decided sequence number",
	}, []string{"identifier", "pubKey"})
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

// SaveHighestInstance saves the state for the highest instance
func (i *ibftStorage) SaveHighestInstance(instance *qbftstorage.StoredInstance) error {
	value, err := instance.Encode()
	if err != nil {
		return errors.Wrap(err, "marshaling error")
	}
	return i.save(value, highestInstanceKey, instance.State.ID)
}

// GetHighestInstance returns the state for the highest instance
func (i *ibftStorage) GetHighestInstance(identifier []byte) (*qbftstorage.StoredInstance, error) {
	val, found, err := i.get(highestInstanceKey, identifier[:])
	if !found {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ret := &qbftstorage.StoredInstance{}
	if err := ret.Decode(val); err != nil {
		return nil, errors.Wrap(err, "un-marshaling error")
	}
	return ret, nil
}

// SaveInstance saves the state for the instance
func (i *ibftStorage) SaveInstance(instance *qbftstorage.StoredInstance) error {
	i.forkLock.RLock()
	defer i.forkLock.RUnlock()

	value, err := instance.Encode()
	if err != nil {
		return errors.Wrap(err, "marshaling error")
	}

	return i.save(value, instanceKey, instance.State.ID, uInt64ToByteSlice(uint64(instance.State.Height)))
}

// GetInstance returns the state for the instance
func (i *ibftStorage) GetInstance(identifier []byte, from specqbft.Height, to specqbft.Height) ([]*qbftstorage.StoredInstance, error) {
	i.forkLock.RLock()
	defer i.forkLock.RUnlock()

	instances := make([]*qbftstorage.StoredInstance, 0)

	for seq := from; seq <= to; seq++ {
		val, found, err := i.get(instanceKey, identifier[:], uInt64ToByteSlice(uint64(seq)))
		if err != nil {
			return instances, err
		}
		if found {
			instance := &qbftstorage.StoredInstance{}
			if err := instance.Decode(val); err != nil {
				return instances, errors.Wrap(err, "could not unmarshal instance")
			}

			instances = append(instances, instance)
			continue
		}
	}

	return instances, nil
}

func (i *ibftStorage) CleanAllInstances(msgID []byte) error {
	i.forkLock.RLock()
	defer i.forkLock.RUnlock()

	prefix := i.prefix
	prefix = append(prefix, msgID[:]...)
	prefix = append(prefix, []byte(instanceKey)...)
	n, err := i.db.DeleteByPrefix(prefix)
	if err != nil {
		return errors.Wrap(err, "failed to remove decided")
	}
	i.logger.Debug("removed decided", zap.Int("count", n),
		zap.String("identifier", hex.EncodeToString(msgID)))
	if err := i.delete(highestInstanceKey, msgID[:]); err != nil {
		return errors.Wrap(err, "failed to remove last decided")
	}
	return nil
}

// SaveLastChangeRoundMsg updates last change round message
func (i *ibftStorage) SaveLastChangeRoundMsg(msg *specqbft.SignedMessage) error {
	i.forkLock.RLock()
	defer i.forkLock.RUnlock()

	var signers [][]byte
	for _, s := range msg.GetSigners() {
		signers = append(signers, uInt64ToByteSlice(uint64(s)))
	}
	signedMsg, err := i.fork.EncodeSignedMsg(msg)
	if err != nil {
		return errors.Wrap(err, "could not encode signed message")
	}
	return i.save(signedMsg, lastChangeRoundKey, msg.Message.Identifier, signers...)
}

// GetLastChangeRoundMsg returns last known change round message
func (i *ibftStorage) GetLastChangeRoundMsg(identifier []byte, signers ...spectypes.OperatorID) ([]*specqbft.SignedMessage, error) {
	i.forkLock.RLock()
	defer i.forkLock.RUnlock()

	if len(signers) == 0 {
		res, err := i.getAll(lastChangeRoundKey, identifier[:])

		if err != nil {
			return nil, err
		}
		return res, nil
	}

	var res []*specqbft.SignedMessage
	for _, s := range signers {
		msg, found, err := i.get(lastChangeRoundKey, identifier[:], uInt64ToByteSlice(uint64(s)))
		if err != nil {
			return res, err
		}
		if !found {
			return res, nil
		}
		sm := new(specqbft.SignedMessage)
		if err := sm.Decode(msg); err != nil {
			return res, err
		}
		res = append(res, sm)
	}
	return res, nil
}

// CleanLastChangeRound cleans last change round message of some validator, should be called upon controller init
func (i *ibftStorage) CleanLastChangeRound(identifier []byte) error {
	i.forkLock.RLock()
	defer i.forkLock.RUnlock()

	prefix := i.prefix
	prefix = append(prefix, identifier[:]...)
	prefix = append(prefix, []byte(lastChangeRoundKey)...)
	n, err := i.db.DeleteByPrefix(prefix)
	if err != nil {
		return errors.Wrap(err, "failed to remove last change round")
	}
	i.logger.Debug("removed last change round", zap.Int("count", n),
		zap.String("identifier", hex.EncodeToString(identifier)))
	return nil
}

func (i *ibftStorage) CleanAllChangeRound() error {
	i.forkLock.RLock()
	defer i.forkLock.RUnlock()

	prefix := i.prefix
	prefix = append(prefix, []byte(lastChangeRoundKey)...)
	n, err := i.db.DeleteByPrefix(prefix)
	if err != nil {
		return errors.Wrap(err, "failed to remove change round")
	}
	i.logger.Debug("removed change round", zap.Int("count", n))
	return nil
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

func (i *ibftStorage) getAll(id string, pk []byte) ([]*specqbft.SignedMessage, error) {
	prefix := append(i.prefix, pk...)
	prefix = append(prefix, id...)

	var res []*specqbft.SignedMessage
	err := i.db.GetAll(prefix, func(i int, obj basedb.Obj) error {
		msg := new(specqbft.SignedMessage)
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
