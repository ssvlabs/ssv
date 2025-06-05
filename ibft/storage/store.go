package storage

import (
	"context"
	"encoding/binary"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/slotticker"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
)

const (
	participantsKey = "pt"
)

// participantStorage struct
// instanceType is what separates different iBFT eth2 duty types (attestation, proposal and aggregation)
type participantStorage struct {
	logger *zap.Logger

	prefix []byte
	db     basedb.Database

	// Participants cache for the current slot. Flushed to DB once every slot.
	cachedParticipants CachedParticipants
	cacheMu            sync.RWMutex
}

type CachedParticipants struct {
	slot         phase0.Slot
	participants map[spectypes.ValidatorPK][]spectypes.OperatorID
}

// New create new participant store
func New(logger *zap.Logger, db basedb.Database, prefix spectypes.BeaconRole, netCfg networkconfig.NetworkConfig, slotTickerProvider slotticker.Provider) qbftstorage.ParticipantStore {
	role := byte(prefix & 0xff)
	st := &participantStorage{
		logger: logger,
		prefix: []byte{role},
		db:     db,
		cachedParticipants: CachedParticipants{
			slot:         netCfg.EstimatedCurrentSlot(),
			participants: make(map[spectypes.ValidatorPK][]spectypes.OperatorID),
		},
	}

	// Persist in-memory participants to DB once every slot.
	slotTicker := slotTickerProvider()
	go st.saveParticipantsJob(slotTicker)

	return st
}

func (st *participantStorage) saveParticipantsJob(slotTicker slotticker.SlotTicker) {
	for range slotTicker.Next() {
		slot := slotTicker.Slot()
		func() {
			// Flush previous slot participants.
			st.cacheMu.Lock()
			defer st.cacheMu.Unlock()

			start := time.Now()
			for pk, participants := range st.cachedParticipants.participants {
				if err := st.saveParticipants(pk, st.cachedParticipants.slot, participants); err != nil {
					st.logger.Error("failed to save participants", fields.Validator(pk[:]), zap.Error(err))
				}
			}

			st.logger.Debug("saved slot participants", fields.Slot(st.cachedParticipants.slot), fields.Took(time.Since(start)))

			// Reset cache for new slot.
			st.cachedParticipants.participants = make(map[spectypes.ValidatorPK][]spectypes.OperatorID)
			st.cachedParticipants.slot = slot
		}()
	}
}

// Prune waits for the initial tick and then removes all slots below the tickSlot - retain
func (i *participantStorage) Prune(ctx context.Context, threshold phase0.Slot) {
	i.logger.Info("start initial stale slot cleanup", zap.String("store", i.ID()), fields.Slot(threshold))

	// remove ALL slots below the threshold
	start := time.Now()
	count := i.removeSlotsOlderThan(threshold)

	i.logger.Info("removed stale slot entries", zap.String("store", i.ID()), fields.Slot(threshold), zap.Int("count", count), zap.Duration("took", time.Since(start)))
}

// PruneContinuously on every tick looks up and removes the slots that fall below the retain threshold
func (i *participantStorage) PruneContinously(ctx context.Context, slotTickerProvider slotticker.Provider, retain phase0.Slot) {
	ticker := slotTickerProvider()
	i.logger.Info("start stale slot cleanup loop", zap.String("store", i.ID()))
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.Next():
			threshold := ticker.Slot() - retain - 1
			count, err := i.removeSlotAt(threshold)
			if err != nil {
				i.logger.Error("remove slot at", zap.String("store", i.ID()), fields.Slot(threshold))
			}

			i.logger.Debug("removed stale slots", zap.String("store", i.ID()), fields.Slot(threshold), zap.Int("count", count))
		}
	}
}

// removes ALL entries that have given slot in their prefix
func (i *participantStorage) removeSlotAt(slot phase0.Slot) (int, error) {
	var keySet [][]byte

	prefix := i.makePrefix(slotToByteSlice(slot))

	tx := i.db.Begin()
	defer tx.Discard()

	// filter and collect keys
	err := i.db.UsingReader(tx).GetAll(prefix, func(i int, o basedb.Obj) error {
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
		if err := i.db.Using(tx).Delete(append(prefix, id...), nil); err != nil {
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
func (i *participantStorage) removeSlotsOlderThan(slot phase0.Slot) int {
	var total int
	for {
		slot-- // slots are incremental
		prefix := i.makePrefix(slotToByteSlice(slot))
		stop := func() bool {
			dropPrefixMu.Lock()
			defer dropPrefixMu.Unlock()

			count, err := i.db.CountPrefix(prefix)
			if err != nil {
				i.logger.Error("count prefix of stale slots", zap.String("store", i.ID()), fields.Slot(slot), zap.Error(err))
				return true
			}

			if count == 0 {
				i.logger.Debug("no more keys at slot", zap.String("store", i.ID()), fields.Slot(slot))
				return true
			}

			if err := i.db.DropPrefix(prefix); err != nil {
				i.logger.Error("drop prefix of stale slots", zap.String("store", i.ID()), fields.Slot(slot), zap.Error(err))
				return true
			}

			i.logger.Debug("drop prefix", zap.String("store", i.ID()), zap.Int64("count", count), fields.Slot(slot))
			total += int(count)

			return false
		}()

		if stop {
			break
		}
	}

	return total
}

func (i *participantStorage) SaveParticipants(pk spectypes.ValidatorPK, slot phase0.Slot, newParticipants []spectypes.OperatorID) (updated bool, err error) {
	start := time.Now()
	defer func() {
		dur := time.Since(start)
		recordSaveDuration(i.ID(), dur)
	}()

	existingParticipants, err := i.getParticipants(pk, slot)
	if err != nil {
		return false, fmt.Errorf("get participants %w", err)
	}

	mergedParticipants := mergeParticipants(existingParticipants, newParticipants)
	if slices.Equal(mergedParticipants, existingParticipants) {
		return false, nil
	}

	// Write to cache or DB.
	i.cacheMu.Lock()
	if i.cachedParticipants.slot != slot {
		i.cacheMu.Unlock()
		if err := i.saveParticipants(pk, slot, mergedParticipants); err != nil {
			return false, fmt.Errorf("save participants: %w", err)
		}
		return true, nil
	}
	i.cachedParticipants.participants[pk] = mergedParticipants
	i.cacheMu.Unlock()

	return true, nil
}

func (i *participantStorage) GetAllParticipantsInRange(from, to phase0.Slot) ([]qbftstorage.ParticipantsRangeEntry, error) {
	var ee []qbftstorage.ParticipantsRangeEntry
	for slot := from; slot <= to; slot++ {
		slotBytes := slotToByteSlice(slot)
		prefix := i.makePrefix(slotBytes)
		err := i.db.GetAll(prefix, func(_ int, o basedb.Obj) error {
			re := qbftstorage.ParticipantsRangeEntry{
				Slot:    slot,
				PubKey:  spectypes.ValidatorPK(o.Key),
				Signers: decodeOperators(o.Value),
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

func (i *participantStorage) GetParticipantsInRange(pk spectypes.ValidatorPK, from, to phase0.Slot) ([]qbftstorage.ParticipantsRangeEntry, error) {
	participantsRange := make([]qbftstorage.ParticipantsRangeEntry, 0)

	for slot := from; slot <= to; slot++ {
		participants, err := i.GetParticipants(pk, slot)
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

func (i *participantStorage) GetParticipants(pk spectypes.ValidatorPK, slot phase0.Slot) ([]spectypes.OperatorID, error) {
	return i.getParticipants(pk, slot)
}

func (i *participantStorage) getParticipants(pk spectypes.ValidatorPK, slot phase0.Slot) ([]spectypes.OperatorID, error) {
	// Check cache first.
	i.cacheMu.RLock()
	if i.cachedParticipants.slot == slot {
		participants, ok := i.cachedParticipants.participants[pk]
		if ok {
			i.cacheMu.RUnlock()
			return participants, nil
		}
	}
	i.cacheMu.RUnlock()

	// Check DB.
	val, found, err := i.get(pk[:], slotToByteSlice(slot))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}

	operators := decodeOperators(val)

	// Update cache.
	i.cacheMu.Lock()
	if i.cachedParticipants.slot == slot {
		i.cachedParticipants.participants[pk] = operators
	}
	i.cacheMu.Unlock()

	return operators, nil
}

func (i *participantStorage) saveParticipants(pk spectypes.ValidatorPK, slot phase0.Slot, operators []spectypes.OperatorID) error {
	bytes, err := encodeOperators(operators)
	if err != nil {
		return fmt.Errorf("encode operators: %w", err)
	}
	if err := i.save(bytes, pk[:], slotToByteSlice(slot)); err != nil {
		return fmt.Errorf("save to DB: %w", err)
	}

	return nil
}

func mergeParticipants(existingParticipants, newParticipants []spectypes.OperatorID) []spectypes.OperatorID {
	allParticipants := slices.Concat(existingParticipants, newParticipants)
	slices.Sort(allParticipants)
	return slices.Compact(allParticipants)
}

func (i *participantStorage) save(value []byte, pk, slot []byte) error {
	prefix := i.makePrefix(slot)
	return i.db.Set(prefix, pk, value)
}

func (i *participantStorage) get(pk, slot []byte) ([]byte, bool, error) {
	prefix := i.makePrefix(slot)
	obj, found, err := i.db.Get(prefix, pk)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, found, nil
	}
	return obj.Value, found, nil
}

func (i *participantStorage) ID() string {
	bnr := spectypes.BeaconRole(uint64(i.prefix[0]))
	return bnr.String()
}

func (i *participantStorage) makePrefix(slot []byte) []byte {
	prefix := make([]byte, 0, len(participantsKey)+1+len(slot))
	prefix = append(prefix, participantsKey...)
	prefix = append(prefix, i.prefix...)
	prefix = append(prefix, slot...)
	return prefix
}

func slotToByteSlice(v phase0.Slot) []byte {
	b := make([]byte, 4)

	// we're casting down but we should be good for now
	slot := uint32(v) // #nosec G115

	binary.LittleEndian.PutUint32(b, slot)
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
