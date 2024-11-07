package storage

import (
	"encoding/binary"
	"fmt"
	"slices"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/exporter/convert"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
)

const (
	highestInstanceKey = "highest_instance"
	instanceKey        = "instance"
	participantsKey    = "participants"
)

var (
	metricsHighestDecided = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:validator:ibft_highest_decided",
		Help: "The highest decided sequence number",
	}, []string{"identifier", "pubKey"})
)

func init() {
	logger := zap.L()
	if err := prometheus.Register(metricsHighestDecided); err != nil {
		logger.Debug("could not register prometheus collector")
	}
}

// ibftStorage struct
// instanceType is what separates different iBFT eth2 duty types (attestation, proposal and aggregation)
type ibftStorage struct {
	prefix         []byte
	db             basedb.Database
	participantsMu sync.Mutex
}

// New create new ibft storage
func New(db basedb.Database, prefix string) qbftstorage.QBFTStore {
	return &ibftStorage{
		prefix: []byte(prefix),
		db:     db,
	}
}

// GetHighestInstance returns the StoredInstance for the highest instance.
func (i *ibftStorage) GetHighestInstance(identifier []byte) (*qbftstorage.StoredInstance, error) {
	val, found, err := i.get(nil, highestInstanceKey, identifier[:])
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

func (i *ibftStorage) SaveInstance(instance *qbftstorage.StoredInstance) error {
	return i.saveInstance(instance, true, false)
}

func (i *ibftStorage) SaveHighestInstance(instance *qbftstorage.StoredInstance) error {
	return i.saveInstance(instance, false, true)
}

func (i *ibftStorage) SaveHighestAndHistoricalInstance(instance *qbftstorage.StoredInstance) error {
	return i.saveInstance(instance, true, true)
}

func (i *ibftStorage) saveInstance(inst *qbftstorage.StoredInstance, toHistory, asHighest bool) error {
	inst.State = instance.CompactCopy(inst.State, inst.DecidedMessage)

	value, err := inst.Encode()
	if err != nil {
		return errors.Wrap(err, "could not encode instance")
	}

	if asHighest {
		err = i.save(nil, value, highestInstanceKey, inst.State.ID)
		if err != nil {
			return errors.Wrap(err, "could not save highest instance")
		}
	}

	if toHistory {
		err = i.save(nil, value, instanceKey, inst.State.ID, uInt64ToByteSlice(uint64(inst.State.Height)))
		if err != nil {
			return errors.Wrap(err, "could not save historical instance")
		}
	}

	return nil
}

// GetInstance returns historical StoredInstance for the given identifier and height.
func (i *ibftStorage) GetInstance(identifier []byte, height specqbft.Height) (*qbftstorage.StoredInstance, error) {
	val, found, err := i.get(nil, instanceKey, identifier[:], uInt64ToByteSlice(uint64(height)))
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

// GetInstancesInRange returns historical StoredInstance's in the given range.
func (i *ibftStorage) GetInstancesInRange(identifier []byte, from specqbft.Height, to specqbft.Height) ([]*qbftstorage.StoredInstance, error) {
	instances := make([]*qbftstorage.StoredInstance, 0)

	for seq := from; seq <= to; seq++ {
		instance, err := i.GetInstance(identifier, seq)
		if err != nil {
			return nil, errors.Wrap(err, "failed to get instance")
		}
		if instance != nil {
			instances = append(instances, instance)
		}
	}

	return instances, nil
}

// CleanAllInstances removes all StoredInstance's & highest StoredInstance's for msgID.
func (i *ibftStorage) CleanAllInstances(msgID []byte) error {
	prefix := i.prefix
	prefix = append(prefix, msgID[:]...)
	prefix = append(prefix, []byte(instanceKey)...)
	err := i.db.DropPrefix(prefix)
	if err != nil {
		return errors.Wrap(err, "failed to remove decided")
	}

	if err := i.delete(nil, highestInstanceKey, msgID[:]); err != nil {
		return errors.Wrap(err, "failed to remove last decided")
	}
	return nil
}

func (i *ibftStorage) UpdateParticipants(identifier convert.MessageID, slot phase0.Slot, newParticipants []spectypes.OperatorID) (updated bool, err error) {
	i.participantsMu.Lock()
	defer i.participantsMu.Unlock()

	txn := i.db.Begin()
	defer txn.Discard()

	existingParticipants, err := i.getParticipants(txn, identifier, slot)
	if err != nil {
		return false, fmt.Errorf("get participants %w", err)
	}

	mergedParticipants := mergeParticipants(existingParticipants, newParticipants)
	if slices.Equal(mergedParticipants, existingParticipants) {
		return false, nil
	}

	if err := i.saveParticipants(txn, identifier, slot, mergedParticipants); err != nil {
		return false, fmt.Errorf("save participants: %w", err)
	}

	if err := txn.Commit(); err != nil {
		return false, fmt.Errorf("commit transaction: %w", err)
	}

	return true, nil
}

func (i *ibftStorage) GetParticipantsInRange(identifier convert.MessageID, from, to phase0.Slot) ([]qbftstorage.ParticipantsRangeEntry, error) {
	participantsRange := make([]qbftstorage.ParticipantsRangeEntry, 0)

	for slot := from; slot <= to; slot++ {
		participants, err := i.GetParticipants(identifier, slot)
		if err != nil {
			return nil, fmt.Errorf("failed to get participants: %w", err)
		}

		if len(participants) == 0 {
			continue
		}

		participantsRange = append(participantsRange, qbftstorage.ParticipantsRangeEntry{
			Slot:       slot,
			Signers:    participants,
			Identifier: identifier,
		})
	}

	return participantsRange, nil
}

func (i *ibftStorage) GetParticipants(identifier convert.MessageID, slot phase0.Slot) ([]spectypes.OperatorID, error) {
	return i.getParticipants(nil, identifier, slot)
}

func (i *ibftStorage) getParticipants(txn basedb.ReadWriter, identifier convert.MessageID, slot phase0.Slot) ([]spectypes.OperatorID, error) {
	val, found, err := i.get(txn, participantsKey, identifier[:], uInt64ToByteSlice(uint64(slot)))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}

	operators := decodeOperators(val)
	return operators, nil
}

func (i *ibftStorage) saveParticipants(txn basedb.ReadWriter, identifier convert.MessageID, slot phase0.Slot, operators []spectypes.OperatorID) error {
	bytes, err := encodeOperators(operators)
	if err != nil {
		return fmt.Errorf("encode operators: %w", err)
	}
	if err := i.save(txn, bytes, participantsKey, identifier[:], uInt64ToByteSlice(uint64(slot))); err != nil {
		return fmt.Errorf("save to DB: %w", err)
	}

	return nil
}

func mergeParticipants(existingParticipants, newParticipants []spectypes.OperatorID) []spectypes.OperatorID {
	allParticipants := slices.Concat(existingParticipants, newParticipants)
	slices.Sort(allParticipants)
	return slices.Compact(allParticipants)
}

func (i *ibftStorage) save(txn basedb.ReadWriter, value []byte, id string, pk []byte, keyParams ...[]byte) error {
	prefix := append(i.prefix, pk...)
	key := i.key(id, keyParams...)
	return i.db.Using(txn).Set(prefix, key, value)
}

func (i *ibftStorage) get(txn basedb.ReadWriter, id string, pk []byte, keyParams ...[]byte) ([]byte, bool, error) {
	prefix := append(i.prefix, pk...)
	key := i.key(id, keyParams...)
	obj, found, err := i.db.Using(txn).Get(prefix, key)
	if !found {
		return nil, found, nil
	}
	if err != nil {
		return nil, found, err
	}
	return obj.Value, found, nil
}

func (i *ibftStorage) delete(txn basedb.ReadWriter, id string, pk []byte, keyParams ...[]byte) error {
	prefix := append(i.prefix, pk...)
	key := i.key(id, keyParams...)
	return i.db.Using(txn).Delete(prefix, key)
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

func encodeOperators(operators []spectypes.OperatorID) ([]byte, error) {
	encoded := make([]byte, len(operators)*8)
	for i, v := range operators {
		binary.BigEndian.PutUint64(encoded[i*8:], v)
	}

	return encoded, nil
}

func decodeOperators(encoded []byte) []spectypes.OperatorID {
	if len(encoded)%8 != 0 {
		panic("corrupted storage: wrong encoded operators length")
	}

	decoded := make([]uint64, len(encoded)/8)
	for i := 0; i < len(decoded); i++ {
		decoded[i] = binary.BigEndian.Uint64(encoded[i*8 : (i+1)*8])
	}

	return decoded
}
