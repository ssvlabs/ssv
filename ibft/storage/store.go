package storage

import (
	"encoding/binary"
	"fmt"
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
	if err != nil {
		return false, fmt.Errorf("get participants %w", err)
	}

	merged := mergeParticipants(existing, newParticipants.ToBitMask())
	if merged == existing {
		return false, nil
	}

	if err := i.saveParticipantsBitMask(txn, identifier, slot, merged); err != nil {
		return false, fmt.Errorf("save participants: %w", err)
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
	committee []spectypes.OperatorID,
) ([]qbftstorage.ParticipantsRangeEntry, error) {
	participantsRange := make([]qbftstorage.ParticipantsRangeEntry, 0)

	for slot := from; slot <= to; slot++ {
		participants, err := i.GetParticipants(identifier, slot, committee)
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
	committee []spectypes.OperatorID,
) ([]spectypes.OperatorID, error) {
	bm, err := i.getParticipantsBitMask(nil, identifier, slot)
	if err != nil {
		return nil, fmt.Errorf("get participants bit mask: %w", err)
	}

	return bm.Signers(committee), nil
}

func (i *ibftStorage) getParticipantsBitMask(
	txn basedb.ReadWriter,
	identifier convert.MessageID,
	slot phase0.Slot,
) (qbftstorage.OperatorsBitMask, error) {
	val, found, err := i.get(txn, participantsKey, identifier[:], uInt64ToByteSlice(uint64(slot)))
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, nil
	}

	return qbftstorage.OperatorsBitMask(byteSliceToUInt16(val)), nil
}

func (i *ibftStorage) saveParticipantsBitMask(
	txn basedb.ReadWriter,
	identifier convert.MessageID,
	slot phase0.Slot,
	operatorsBitMask qbftstorage.OperatorsBitMask,
) error {
	b := uInt16ToByteSlice(uint16(operatorsBitMask))
	if err := i.save(txn, b, participantsKey, identifier[:], uInt64ToByteSlice(uint64(slot))); err != nil {
		return fmt.Errorf("save to DB: %w", err)
	}

	return nil
}

func mergeParticipants(existingParticipants, newParticipants qbftstorage.OperatorsBitMask) qbftstorage.OperatorsBitMask {
	return existingParticipants | newParticipants
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
