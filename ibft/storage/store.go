package storage

import (
	"encoding/binary"
	"encoding/hex"
	"log"
	"sync"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
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

// SaveHighestInstance saves the StoredInstance for the highest instance.
func (i *ibftStorage) SaveHighestInstance(instance *qbftstorage.StoredInstance) error {
	value, err := instance.Encode()
	if err != nil {
		return errors.Wrap(err, "could not encode instance")
	}
	return i.save(value, highestInstanceKey, instance.State.ID)
}

// GetHighestInstance returns the StoredInstance for the highest instance.
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
		return nil, errors.Wrap(err, "could not decode instance")
	}
	return ret, nil
}

// SaveInstance saves historical StoredInstance.
func (i *ibftStorage) SaveInstance(instance *qbftstorage.StoredInstance) error {
	i.forkLock.RLock()
	defer i.forkLock.RUnlock()

	value, err := instance.Encode()
	if err != nil {
		return errors.Wrap(err, "could not encode instance")
	}

	return i.save(value, instanceKey, instance.State.ID, uInt64ToByteSlice(uint64(instance.State.Height)))
}

// GetInstancesInRange returns historical StoredInstance's in the given range.
func (i *ibftStorage) GetInstancesInRange(identifier []byte, from specqbft.Height, to specqbft.Height) ([]*qbftstorage.StoredInstance, error) {
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
				return instances, errors.Wrap(err, "could not decode instance")
			}

			instances = append(instances, instance)
			continue
		}
	}

	return instances, nil
}

// CleanAllInstances removes all StoredInstance's & highest StoredInstance's for msgID.
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

// CleanAllChangeRound removes all change round messages from storage.
// NOTE: It needs to be kept for migration.
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
