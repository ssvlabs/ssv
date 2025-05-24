package storage

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter/convert"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
)

const (
	highestInstanceKey = "highest_instance"
	instanceKey        = "instance"
	participantsKey    = "participants"
	committeesKey      = "committees"
)

var errNotBitmask = errors.New("not a bitmask")

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

func (i *ibftStorage) UpdateParticipants(identifier convert.MessageID, slot phase0.Slot, newParticipants qbftstorage.Quorum) (updated bool, err error) {
	i.participantsMu.Lock()
	defer i.participantsMu.Unlock()

	txn := i.db.Begin()
	defer txn.Discard()

	existing, err := i.getParticipantsBitMask(txn, identifier, slot)
	if errors.Is(err, errNotBitmask) {
		existingList, err := i.getParticipants(txn, identifier, slot)
		if err != nil {
			return false, fmt.Errorf("get participants: %w", err)
		}

		quorumAdapter, err := qbftstorage.NewQuroum(existingList, newParticipants.Committee)
		if err != nil {
			return false, fmt.Errorf("create quorum adapter: %w", err)
		}

		existing = quorumAdapter.ToSignersBitMask()
	} else if err != nil {
		return false, fmt.Errorf("get participants bitmask: %w", err)
	}

	merged := mergeParticipantsBitMask(existing, newParticipants.ToSignersBitMask())
	if merged == existing {
		return false, nil
	}

	if err := i.saveParticipantsBitMask(txn, identifier, slot, merged); err != nil {
		return false, fmt.Errorf("save participants bitmask: %w", err)
	}

	if err := i.saveCommittee(txn, identifier, slot, newParticipants.Committee); err != nil {
		return false, fmt.Errorf("save committee: %w", err)
	}

	if err := txn.Commit(); err != nil {
		return false, fmt.Errorf("commit transaction: %w", err)
	}

	return true, nil
}

func (i *ibftStorage) GetParticipantsInRange(
	identifier convert.MessageID,
	from,
	to phase0.Slot,
) ([]qbftstorage.ParticipantsRangeEntry, error) {
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

func (i *ibftStorage) GetParticipants(
	identifier convert.MessageID,
	slot phase0.Slot,
) ([]spectypes.OperatorID, error) {
	bm, err := i.getParticipantsBitMask(nil, identifier, slot)
	if errors.Is(err, errNotBitmask) {
		return i.getParticipants(nil, identifier, slot)
	}
	if err != nil {
		return nil, fmt.Errorf("get participants bit mask: %w", err)
	}

	committee, err := i.getCommittee(nil, identifier, slot)
	if err != nil {
		return nil, fmt.Errorf("get committee: %w", err)
	}

	return bm.Signers(committee)
}

func (i *ibftStorage) getParticipantsBitMask(
	txn basedb.ReadWriter,
	identifier convert.MessageID,
	slot phase0.Slot,
) (qbftstorage.SignersBitMask, error) {
	val, found, err := i.get(txn, participantsKey, identifier[:], uInt64ToByteSlice(uint64(slot)))
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, nil
	}

	if len(val) != 2 { // uint16
		return 0, errNotBitmask
	}

	return qbftstorage.SignersBitMask(byteSliceToUInt16(val)), nil
}

// DEPRECATED, left for compatibility with old data format
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

func (i *ibftStorage) saveParticipantsBitMask(
	txn basedb.ReadWriter,
	identifier convert.MessageID,
	slot phase0.Slot,
	operatorsBitMask qbftstorage.SignersBitMask,
) error {
	b := uInt16ToByteSlice(uint16(operatorsBitMask))
	if err := i.save(txn, b, participantsKey, identifier[:], uInt64ToByteSlice(uint64(slot))); err != nil {
		return fmt.Errorf("save to DB: %w", err)
	}

	return nil
}

// mergeParticipantsBitMask merges two participants bitmasks. Extracted into a method for testing.
func mergeParticipantsBitMask(existingParticipants, newParticipants qbftstorage.SignersBitMask) qbftstorage.SignersBitMask {
	return existingParticipants | newParticipants
}

func (i *ibftStorage) getCommittee(
	txn basedb.ReadWriter,
	identifier convert.MessageID,
	slot phase0.Slot,
) ([]spectypes.OperatorID, error) {
	val, found, err := i.get(txn, committeesKey, identifier[:])
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	operatorsBySlot := decodeOperatorsBySlot(val)
	if len(operatorsBySlot) == 0 {
		return nil, fmt.Errorf("no operators found for identifier %s", identifier.String())
	}

	slots := slices.Sorted(maps.Keys(operatorsBySlot))
	for idx := len(slots) - 1; idx >= 0; idx-- {
		if slot >= slots[idx] {
			return operatorsBySlot[slots[idx]], nil
		}
	}

	return nil, fmt.Errorf("no operators found for slot %d, seen slots %v", slot, slots)
}

func (i *ibftStorage) saveCommittee(
	txn basedb.ReadWriter,
	identifier convert.MessageID,
	slot phase0.Slot,
	operators []spectypes.OperatorID,
) error {
	val, found, err := i.get(txn, committeesKey, identifier[:])
	if err != nil {
		return err
	}

	operatorsBySlot := make(map[phase0.Slot][]spectypes.OperatorID)
	if found {
		operatorsBySlot = decodeOperatorsBySlot(val)
	}

	if changed := i.upsertSlot(operatorsBySlot, slot, operators); !changed {
		return nil
	}

	bytes, err := encodeOperatorsBySlot(operatorsBySlot)
	if err != nil {
		return fmt.Errorf("encode operators: %w", err)
	}

	if err := i.save(txn, bytes, committeesKey, identifier[:]); err != nil {
		return fmt.Errorf("save to DB: %w", err)
	}

	return nil
}

func (i *ibftStorage) upsertSlot(
	operatorsBySlot map[phase0.Slot][]spectypes.OperatorID,
	slot phase0.Slot,
	operators []spectypes.OperatorID,
) bool {
	slots := slices.Sorted(maps.Keys(operatorsBySlot))
	if len(slots) == 0 {
		operatorsBySlot[slot] = operators
		return true
	}

	if slot < slots[0] {
		// slot is earlier than any existing => move the first slot or insert the new slot
		if slices.Equal(operators, operatorsBySlot[slots[0]]) {
			// first slot has same operators => move the first slot
			operatorsBySlot[slot] = operatorsBySlot[slots[0]]
			delete(operatorsBySlot, slots[0])
			return true
		}

		// first slot has different operators => insert new slot
		operatorsBySlot[slot] = operators
		return true
	}

	for i := len(slots) - 1; i >= 0; i-- {
		if slot < slots[i] {
			continue
		}

		if slices.Equal(operators, operatorsBySlot[slots[i]]) {
			// operators are same => sequence is fine, nothing to do
			return false
		}

		if i+1 < len(slots) && slices.Equal(operators, operatorsBySlot[slots[i+1]]) {
			// next slot exists and has same operators => move the next slot earlier
			operatorsBySlot[slot] = operatorsBySlot[slots[i+1]]
			delete(operatorsBySlot, slots[i+1])
			return true
		}

		// slot is between existing slots with different operators => create a new entry for the slot
		operatorsBySlot[slot] = operators
		return true
	}

	return false
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

func uInt16ToByteSlice(n uint16) []byte {
	b := make([]byte, 2)
	binary.LittleEndian.PutUint16(b, n)
	return b
}

func byteSliceToUInt16(b []byte) uint16 {
	return binary.LittleEndian.Uint16(b)
}

// DEPRECATED, left for compatibility with old data format (tests only)
func encodeOperators(operators []spectypes.OperatorID) ([]byte, error) {
	encoded := make([]byte, len(operators)*8)
	for i, v := range operators {
		binary.BigEndian.PutUint64(encoded[i*8:], v)
	}

	return encoded, nil
}

// DEPRECATED, left for compatibility with old data format
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

func encodeOperatorsBySlot(operatorsBySlot map[phase0.Slot][]spectypes.OperatorID) ([]byte, error) {
	return json.Marshal(operatorsBySlot)
}

func decodeOperatorsBySlot(encoded []byte) map[phase0.Slot][]spectypes.OperatorID {
	var operatorsBySlot map[phase0.Slot][]spectypes.OperatorID

	if err := json.Unmarshal(encoded, &operatorsBySlot); err != nil {
		panic("corrupted storage: operators by slot could not be decoded")
	}

	return operatorsBySlot
}
