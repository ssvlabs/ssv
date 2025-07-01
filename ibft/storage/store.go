package storage

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/operator/slotticker"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
)

const (
	highestInstanceKey = "highest_instance"
	instanceKey        = "instance"
	participantsKey    = "pt"
	committeesKey      = "committees"
)

var errNotBitmask = errors.New("not a bitmask")

// participantStorage struct
// instanceType is what separates different iBFT eth2 duty types (attestation, proposal and aggregation)
type participantStorage struct {
	logger         *zap.Logger
	prefix         []byte
	oldPrefix      string // kept back for cleanup
	db             basedb.Database
	participantsMu sync.Mutex
}

// New create new participant store
func New(logger *zap.Logger, db basedb.Database, prefix spectypes.BeaconRole) qbftstorage.ParticipantStore {
	role := byte(prefix & 0xff)
	return &participantStorage{
		logger:    logger,
		prefix:    []byte{role},
		oldPrefix: prefix.String(),
		db:        db,
	}
}

// Prune waits for the initial tick and then removes all slots below the tickSlot - retain
func (ps *participantStorage) Prune(ctx context.Context, threshold phase0.Slot) {
	ps.logger.Info("start initial stale slot cleanup", zap.String("store", ps.ID()), fields.Slot(threshold))

	// remove ALL slots below the threshold
	start := time.Now()
	count := ps.removeSlotsOlderThan(threshold)

	ps.logger.Info("removed stale slot entries", zap.String("store", ps.ID()), fields.Slot(threshold), zap.Int("count", count), zap.Duration("took", time.Since(start)))
}

// PruneContinuously on every tick looks up and removes the slots that fall below the retain threshold
func (ps *participantStorage) PruneContinously(ctx context.Context, slotTickerProvider slotticker.Provider, retain phase0.Slot) {
	ticker := slotTickerProvider()
	ps.logger.Info("start stale slot cleanup loop", zap.String("store", ps.ID()))
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.Next():
			threshold := ticker.Slot() - retain - 1
			count, err := ps.removeSlotAt(threshold)
			if err != nil {
				ps.logger.Error("remove slot at", zap.String("store", ps.ID()), fields.Slot(threshold))
			}

			ps.logger.Debug("removed stale slots", zap.String("store", ps.ID()), fields.Slot(threshold), zap.Int("count", count))
		}
	}
}

// removes ALL entries that have given slot in their prefix
func (ps *participantStorage) removeSlotAt(slot phase0.Slot) (int, error) {
	var keySet [][]byte

	prefix := ps.makeParticipantsPrefix(slotToByteSlice(slot))

	tx := ps.db.Begin()
	defer tx.Discard()

	// filter and collect keys
	err := ps.db.UsingReader(tx).GetAll(prefix, func(i int, o basedb.Obj) error {
		keySet = append(keySet, o.Key)
		return nil
	})

	if err != nil {
		return 0, fmt.Errorf("collect keys of stale slots: %w", err)
	}

	if len(keySet) == 0 {
		return 0, nil
	}

	for _, id := range keySet {
		if err := ps.db.Using(tx).Delete(append(prefix, id...), nil); err != nil {
			return 0, fmt.Errorf("remove slot: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit old slot removal: %w", err)
	}

	return len(keySet), nil
}

var dropPrefixMu sync.Mutex

// removes ALL entries for any slots older or equal to given slot
func (ps *participantStorage) removeSlotsOlderThan(slot phase0.Slot) int {
	var total int
	for {
		slot-- // slots are incremental
		prefix := ps.makeParticipantsPrefix(slotToByteSlice(slot))
		stop := func() bool {
			dropPrefixMu.Lock()
			defer dropPrefixMu.Unlock()

			count, err := ps.db.CountPrefix(prefix)
			if err != nil {
				ps.logger.Error("count prefix of stale slots", zap.String("store", ps.ID()), fields.Slot(slot), zap.Error(err))
				return true
			}

			if count == 0 {
				ps.logger.Debug("no more keys at slot", zap.String("store", ps.ID()), fields.Slot(slot))
				return true
			}

			if err := ps.db.DropPrefix(prefix); err != nil {
				ps.logger.Error("drop prefix of stale slots", zap.String("store", ps.ID()), fields.Slot(slot), zap.Error(err))
				return true
			}

			ps.logger.Debug("drop prefix", zap.String("store", ps.ID()), zap.Int64("count", count), fields.Slot(slot))
			total += int(count)

			return false
		}()

		if stop {
			break
		}
	}

	return total
}

// CleanAllInstances removes all records in old format.
func (ps *participantStorage) CleanAllInstances() error {
	if err := ps.db.DropPrefix([]byte(ps.oldPrefix)); err != nil {
		return errors.Wrap(err, "failed to drop all records")
	}

	return nil
}

func (ps *participantStorage) SaveParticipants(
	pk spectypes.ValidatorPK,
	slot phase0.Slot,
	newParticipants qbftstorage.Quorum,
) (updated bool, err error) {
	start := time.Now()
	defer func() {
		dur := time.Since(start)
		recordSaveDuration(ps.ID(), dur)
	}()

	ps.participantsMu.Lock()
	defer ps.participantsMu.Unlock()

	txn := ps.db.Begin()
	defer txn.Discard()

	existing, err := ps.getParticipantsBitMask(txn, pk, slot)
	if errors.Is(err, errNotBitmask) {
		existingList, err := ps.getParticipants(txn, pk, slot)
		if err != nil {
			return false, fmt.Errorf("get participants: %w", err)
		}

		quorumAdapter, err := qbftstorage.NewQuorum(existingList, newParticipants.Committee)
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

	// saveParticipantsBitMask & saveCommittee change the data format to the new one
	// (storing participants bitmask + committee instead of signers),
	// so it's only one-way compatible and the old code won't be able to read the new format
	if err := ps.saveParticipantsBitMask(txn, pk, slot, merged); err != nil {
		return false, fmt.Errorf("save participants bitmask: %w", err)
	}

	if err := ps.saveCommittee(txn, pk, slot, newParticipants.Committee); err != nil {
		return false, fmt.Errorf("save committee: %w", err)
	}

	if err := txn.Commit(); err != nil {
		return false, fmt.Errorf("commit transaction: %w", err)
	}

	return true, nil
}

func (ps *participantStorage) GetAllParticipantsInRange(from, to phase0.Slot) ([]qbftstorage.ParticipantsRangeEntry, error) {
	var ee []qbftstorage.ParticipantsRangeEntry
	for slot := from; slot <= to; slot++ {
		slotBytes := slotToByteSlice(slot)
		prefix := ps.makeParticipantsPrefix(slotBytes)
		err := ps.db.GetAll(prefix, func(_ int, o basedb.Obj) error {
			signersBitMask := qbftstorage.SignersBitMask(byteSliceToUInt16(o.Value))

			pk := o.Key
			if len(pk) != len(spectypes.ValidatorPK{}) {
				return fmt.Errorf("invalid pk length: %d", len(pk))
			}

			committee, err := ps.getCommittee(nil, spectypes.ValidatorPK(pk), slot)
			if err != nil {
				return fmt.Errorf("get committee: %w", err)
			}

			signers, err := signersBitMask.Signers(committee)
			if err != nil {
				return fmt.Errorf("get signers: %w", err)
			}

			re := qbftstorage.ParticipantsRangeEntry{
				Slot:    slot,
				PubKey:  spectypes.ValidatorPK(o.Key),
				Signers: signers,
			}
			ee = append(ee, re)
			return nil
		})

		if err != nil {
			return nil, err
		}
	}

	return ee, nil
}

func (ps *participantStorage) GetParticipantsInRange(pk spectypes.ValidatorPK, from, to phase0.Slot) ([]qbftstorage.ParticipantsRangeEntry, error) {
	participantsRange := make([]qbftstorage.ParticipantsRangeEntry, 0)

	for slot := from; slot <= to; slot++ {
		participants, err := ps.GetParticipants(pk, slot)
		if err != nil {
			return nil, fmt.Errorf("failed to get participants: %w", err)
		}

		if len(participants) == 0 {
			continue
		}

		participantsRange = append(participantsRange, qbftstorage.ParticipantsRangeEntry{
			Slot:    slot,
			PubKey:  pk,
			Signers: participants,
		})
	}

	return participantsRange, nil
}

func (ps *participantStorage) GetParticipants(pk spectypes.ValidatorPK, slot phase0.Slot) ([]spectypes.OperatorID, error) {
	bm, err := ps.getParticipantsBitMask(nil, pk, slot)
	if errors.Is(err, errNotBitmask) {
		return ps.getParticipants(nil, pk, slot)
	}
	if err != nil {
		return nil, fmt.Errorf("get participants bit mask: %w", err)
	}

	committee, err := ps.getCommittee(nil, pk, slot)
	if err != nil {
		return nil, fmt.Errorf("get committee: %w", err)
	}

	return bm.Signers(committee)
}

func (ps *participantStorage) getParticipantsBitMask(
	txn basedb.ReadWriter,
	pk spectypes.ValidatorPK,
	slot phase0.Slot,
) (qbftstorage.SignersBitMask, error) {
	val, found, err := ps.getParticipantsAtSlot(txn, pk[:], slotToByteSlice(slot))
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
func (ps *participantStorage) getParticipants(txn basedb.ReadWriter, pk spectypes.ValidatorPK, slot phase0.Slot) ([]spectypes.OperatorID, error) {
	val, found, err := ps.getParticipantsAtSlot(txn, pk[:], slotToByteSlice(slot))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}

	operators := decodeOperators(val)
	return operators, nil
}

func (ps *participantStorage) saveParticipantsBitMask(
	txn basedb.ReadWriter,
	pk spectypes.ValidatorPK,
	slot phase0.Slot,
	operatorsBitMask qbftstorage.SignersBitMask,
) error {
	b := uInt16ToByteSlice(uint16(operatorsBitMask))
	if err := ps.saveParticipantsAtSlot(txn, b, pk[:], slotToByteSlice(slot)); err != nil {
		return fmt.Errorf("save to DB: %w", err)
	}

	return nil
}

func mergeParticipantsBitMask(existingParticipants, newParticipants qbftstorage.SignersBitMask) qbftstorage.SignersBitMask {
	return existingParticipants | newParticipants
}

func (ps *participantStorage) getCommittee(
	txn basedb.ReadWriter,
	pk spectypes.ValidatorPK,
	slot phase0.Slot,
) ([]spectypes.OperatorID, error) {
	val, found, err := ps.getCommitteeForPK(txn, pk[:])
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	committeeBySlot := decodeCommitteeBySlot(val)
	if len(committeeBySlot) == 0 {
		return nil, fmt.Errorf("no committee found for pk %s", pk)
	}

	committeeSlots := slices.Collect(maps.Keys(committeeBySlot))
	slices.Sort(committeeSlots)
	idx, ok := slices.BinarySearch(committeeSlots, slot)
	switch {
	case ok:
		// exact start match
		return committeeBySlot[committeeSlots[idx]], nil

	case idx == 0:
		// before the very first interval
		return nil, fmt.Errorf("no committee for slot %d (earlier than first %d)", slot, committeeSlots[0])

	default:
		// predecessor interval
		return committeeBySlot[committeeSlots[idx-1]], nil
	}
}

func (ps *participantStorage) saveCommittee(
	txn basedb.ReadWriter,
	pk spectypes.ValidatorPK,
	slot phase0.Slot,
	operators []spectypes.OperatorID,
) error {
	val, found, err := ps.getCommitteeForPK(txn, pk[:])
	if err != nil {
		return err
	}

	committeeBySlot := make(map[phase0.Slot][]spectypes.OperatorID)
	if found {
		committeeBySlot = decodeCommitteeBySlot(val)
	}

	if changed := ps.upsertSlot(committeeBySlot, slot, operators); !changed {
		return nil
	}

	bytes, err := encodeCommitteeBySlot(committeeBySlot)
	if err != nil {
		return fmt.Errorf("encode operators: %w", err)
	}

	if err := ps.saveCommitteeForPK(txn, bytes, pk[:]); err != nil {
		return fmt.Errorf("save to DB: %w", err)
	}

	return nil
}

func (ps *participantStorage) upsertSlot(
	committeeBySlot map[phase0.Slot][]spectypes.OperatorID,
	slot phase0.Slot,
	committee []spectypes.OperatorID,
) bool {
	slots := slices.Collect(maps.Keys(committeeBySlot))
	if len(slots) == 0 {
		committeeBySlot[slot] = committee
		return true
	}

	slices.Sort(slots)

	// 3. Locate slot
	idx, ok := slices.BinarySearch(slots, slot)

	// 4. Exact-boundary update
	if ok {
		if slices.Equal(committeeBySlot[slot], committee) {
			return false // no change
		}
		committeeBySlot[slot] = committee
		return true
	}

	// 5. Fetch predecessor and successor slices (if any)
	var prev, next []spectypes.OperatorID
	if idx > 0 {
		prev = committeeBySlot[slots[idx-1]]
	}
	if idx < len(slots) {
		next = committeeBySlot[slots[idx]]
	}

	// 6. Merge with predecessor?
	if prev != nil && slices.Equal(prev, committee) {
		return false // extending prev interval implicitly
	}

	// 7. Merge with successor?
	if next != nil && slices.Equal(next, committee) {
		// move successor boundary backward
		delete(committeeBySlot, slots[idx])
		committeeBySlot[slot] = next
		return true
	}

	// 8. No merge: insert new boundary
	committeeBySlot[slot] = committee
	return true
}

func (ps *participantStorage) saveParticipantsAtSlot(txn basedb.ReadWriter, value []byte, pk, slot []byte) error {
	prefix := ps.makeParticipantsPrefix(slot)
	return ps.db.Using(txn).Set(prefix, pk, value)
}

func (ps *participantStorage) getParticipantsAtSlot(txn basedb.ReadWriter, pk, slot []byte) ([]byte, bool, error) {
	prefix := ps.makeParticipantsPrefix(slot)
	obj, found, err := ps.db.Using(txn).Get(prefix, pk)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, found, nil
	}
	return obj.Value, found, nil
}

func (ps *participantStorage) saveCommitteeForPK(txn basedb.ReadWriter, value []byte, pk []byte) error {
	prefix := ps.makeCommitteesPrefix()
	return ps.db.Using(txn).Set(prefix, pk, value)
}

func (ps *participantStorage) getCommitteeForPK(txn basedb.ReadWriter, pk []byte) ([]byte, bool, error) {
	prefix := ps.makeCommitteesPrefix()
	obj, found, err := ps.db.Using(txn).Get(prefix, pk)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, found, nil
	}
	return obj.Value, found, nil
}

func (ps *participantStorage) ID() string {
	bnr := spectypes.BeaconRole(ps.prefix[0])
	return bnr.String()
}

func (ps *participantStorage) makeParticipantsPrefix(slot []byte) []byte {
	prefix := make([]byte, 0, len(participantsKey)+1+len(slot))
	prefix = append(prefix, participantsKey...)
	prefix = append(prefix, ps.prefix...)
	prefix = append(prefix, slot...)
	return prefix
}

func (ps *participantStorage) makeCommitteesPrefix() []byte {
	prefix := make([]byte, 0, len(committeesKey)+1)
	prefix = append(prefix, committeesKey...)
	prefix = append(prefix, ps.prefix...)
	return prefix
}

func slotToByteSlice(v phase0.Slot) []byte {
	b := make([]byte, 4)

	// we're casting down but we should be good for now
	slot := uint32(v) // #nosec G115

	binary.LittleEndian.PutUint32(b, slot)
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

func encodeCommitteeBySlot(committeeBySlot map[phase0.Slot][]spectypes.OperatorID) ([]byte, error) {
	return json.Marshal(committeeBySlot)
}

func decodeCommitteeBySlot(encoded []byte) map[phase0.Slot][]spectypes.OperatorID {
	var committeeBySlot map[phase0.Slot][]spectypes.OperatorID

	if err := json.Unmarshal(encoded, &committeeBySlot); err != nil {
		panic("corrupted storage: committee by slot could not be decoded")
	}

	return committeeBySlot
}
