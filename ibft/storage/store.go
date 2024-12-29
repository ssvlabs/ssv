package storage

import (
	"context"
	"encoding/binary"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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

// participantStorage struct
// instanceType is what separates different iBFT eth2 duty types (attestation, proposal and aggregation)
type participantStorage struct {
	prefix         []byte
	oldPrefix      string // kept back for cleanup
	db             basedb.Database
	participantsMu sync.Mutex
}

// New create new participant store
func New(db basedb.Database, prefix spectypes.BeaconRole) qbftstorage.ParticipantStore {
	role := byte(prefix & 0xff)
	return &participantStorage{
		prefix:    []byte{role},
		oldPrefix: prefix.String(),
		db:        db,
	}
}

func (i *participantStorage) StartCleanupJob(ctx context.Context, logger *zap.Logger, slotTickerProvider slotticker.Provider, retain int) {
	ticker := slotTickerProvider()
	<-ticker.Next()
	threashold := ticker.Slot() - phase0.Slot(retain) // #nosec G115

	logger.Info("start initial stale slot cleanup", fields.Slot(threashold), zap.String("store", i.ID()), zap.Int("retain", retain))

	// on start we remove ALL slots below the threashold
	start := time.Now()
	count := i.removeSlotsOlderThan(logger, threashold)

	logger.Info("removed stale slot entries", fields.Slot(threashold), zap.String("store", i.ID()), zap.Int("count", count), zap.Duration("took", time.Since(start)))

	go func() {
		logger.Info("start stale slot cleanup background job", zap.String("store", i.ID()))
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.Next():
				threashold := ticker.Slot() - phase0.Slot(retain) - 1 // #nosec G115
				count, err := i.removeSlotAt(threashold)
				if err != nil {
					logger.Error("remove slot at", fields.Slot(threashold))
				}

				logger.Debug("removed stale slot", fields.Slot(threashold), zap.Int("count", count), zap.String("store", i.ID()))
			}
		}
	}()
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
func (i *participantStorage) removeSlotsOlderThan(logger *zap.Logger, slot phase0.Slot) int {
	current := slot
	begin := time.Now()
	var total int
	for {
		current-- // slots are incremental
		prefix := i.makePrefix(slotToByteSlice(current))
		stop := func() bool {
			dropPrefixMu.Lock()
			defer dropPrefixMu.Unlock()
			start := time.Now()

			count, err := i.db.CountPrefix(prefix)
			if err != nil {
				logger.Error("count prefix of stale slots", zap.Error(err), fields.Slot(current))
				return true
			}

			logger.Debug("count prefix", zap.String("store", i.ID()), fields.Took(time.Since(start)), zap.Int64("count", count), fields.Slot(current))

			if count == 0 {
				logger.Debug("no more keys at slot", zap.String("store", i.ID()), fields.Slot(current))
				return true
			}

			start = time.Now()
			if err := i.db.DropPrefix(prefix); err != nil {
				logger.Error("drop prefix of stale slots", zap.Error(err), fields.Slot(current))
				return true
			}

			logger.Debug("drop prefix", zap.String("store", i.ID()), fields.Took(time.Since(start)), zap.Int64("count", count), fields.Slot(current))
			total += int(count)

			return false
		}()

		if stop {
			logger.Debug("done cleanup stale slots", zap.String("store", i.ID()), zap.Int("total", total), fields.Took(time.Since(begin)))
			break
		}
	}

	return total
}

// CleanAllInstances removes all records in old format.
func (i *participantStorage) CleanAllInstances() error {
	if err := i.db.DropPrefix([]byte(i.oldPrefix)); err != nil {
		return errors.Wrap(err, "failed to drop all records")
	}

	return nil
}

func (i *participantStorage) UpdateParticipants(pk spectypes.ValidatorPK, slot phase0.Slot, newParticipants []spectypes.OperatorID) (updated bool, err error) {
	i.participantsMu.Lock()
	defer i.participantsMu.Unlock()

	txn := i.db.Begin()
	defer txn.Discard()

	existingParticipants, err := i.getParticipants(txn, pk, slot)
	if err != nil {
		return false, fmt.Errorf("get participants %w", err)
	}

	mergedParticipants := mergeParticipants(existingParticipants, newParticipants)
	if slices.Equal(mergedParticipants, existingParticipants) {
		return false, nil
	}

	if err := i.saveParticipants(txn, pk, slot, mergedParticipants); err != nil {
		return false, fmt.Errorf("save participants: %w", err)
	}

	if err := txn.Commit(); err != nil {
		return false, fmt.Errorf("commit transaction: %w", err)
	}

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
	return i.getParticipants(nil, pk, slot)
}

func (i *participantStorage) getParticipants(txn basedb.ReadWriter, pk spectypes.ValidatorPK, slot phase0.Slot) ([]spectypes.OperatorID, error) {
	val, found, err := i.get(txn, pk[:], slotToByteSlice(slot))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}

	operators := decodeOperators(val)
	return operators, nil
}

func (i *participantStorage) saveParticipants(txn basedb.ReadWriter, pk spectypes.ValidatorPK, slot phase0.Slot, operators []spectypes.OperatorID) error {
	bytes, err := encodeOperators(operators)
	if err != nil {
		return fmt.Errorf("encode operators: %w", err)
	}
	if err := i.save(txn, bytes, pk[:], slotToByteSlice(slot)); err != nil {
		return fmt.Errorf("save to DB: %w", err)
	}

	return nil
}

func mergeParticipants(existingParticipants, newParticipants []spectypes.OperatorID) []spectypes.OperatorID {
	allParticipants := slices.Concat(existingParticipants, newParticipants)
	slices.Sort(allParticipants)
	return slices.Compact(allParticipants)
}

func (i *participantStorage) save(txn basedb.ReadWriter, value []byte, pk, slot []byte) error {
	prefix := i.makePrefix(slot)
	return i.db.Using(txn).Set(prefix, pk, value)
}

func (i *participantStorage) get(txn basedb.ReadWriter, pk, slot []byte) ([]byte, bool, error) {
	prefix := i.makePrefix(slot)
	obj, found, err := i.db.Using(txn).Get(prefix, pk)
	if !found {
		return nil, found, nil
	}
	if err != nil {
		return nil, found, err
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
	slot := uint32(uint64(v)) // #nosec G115

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
