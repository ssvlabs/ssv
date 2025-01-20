package store

import (
	"encoding/binary"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	model "github.com/ssvlabs/ssv/exporter/v2"
	"github.com/ssvlabs/ssv/storage/basedb"
)

const (
	validatorDutyTraceKey = "vd"
	commiteeDutyTraceKey  = "cd"
	commiteeVIndexKey     = "ci"
)

type DutyTraceStore struct {
	db basedb.Database
}

func New(db basedb.Database) *DutyTraceStore {
	return &DutyTraceStore{
		db: db,
	}
}

func (s *DutyTraceStore) GetValidatorDuties(role spectypes.BeaconRole, slot phase0.Slot, index phase0.ValidatorIndex) (out []*model.ValidatorDutyTrace, err error) {
	prefix := s.makeValidatorSlotPrefix(role, slot, index)
	err = s.db.GetAll(prefix, func(_ int, o basedb.Obj) error {
		vdt := new(model.ValidatorDutyTrace)
		err := vdt.UnmarshalSSZ(o.Value)
		if err != nil {
			return fmt.Errorf("unmarshall ValidatorDutyTrace: %w", err)
		}
		out = append(out, vdt)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return
}

func (s *DutyTraceStore) GetAllValidatorDuties(role spectypes.BeaconRole, slot phase0.Slot) (out []*model.ValidatorDutyTrace, err error) {
	panic("fix me")
}

func (s *DutyTraceStore) GetCommitteeDuties(slot phase0.Slot, role spectypes.BeaconRole) (out []*model.CommitteeDutyTrace, err error) {
	panic("fix me")
}

func (s *DutyTraceStore) GetCommitteeDutiesByValidator(indexes []phase0.ValidatorIndex, slot phase0.Slot) (out []*model.CommitteeDutyTrace, err error) {
	prefixes := s.makeCommiteeValidatorIndexPrefixes(indexes, slot)
	keys := make([][]byte, 0)

	tx := s.db.BeginRead()
	defer tx.Discard()

	for _, prefix := range prefixes {
		err = s.db.GetAll(prefix, func(_ int, o basedb.Obj) error {
			keys = append(keys, o.Value)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	err = s.db.GetMany(nil, keys, func(o basedb.Obj) error {
		vdt := new(model.CommitteeDutyTrace)
		if err := vdt.UnmarshalSSZ(o.Value); err != nil {
			return fmt.Errorf("unmarshall ValidatorDutyTrace: %w", err)
		}
		out = append(out, vdt)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return
}

func (s *DutyTraceStore) SaveValidatorDuty(dto *model.ValidatorDutyTrace) error {
	prefix := s.makeValidatorPrefix(dto)

	value, err := dto.MarshalSSZ()
	if err != nil {
		return fmt.Errorf("marshall ValidatorDutyTrace: %w", err)
	}

	tx := s.db.Begin()
	defer tx.Discard()

	err = s.db.Using(tx).Set(prefix, nil, value)
	if err != nil {
		return fmt.Errorf("save full ValidatorDutyTrace: %w", err)
	}

	return tx.Commit()
}

func (s *DutyTraceStore) SaveCommiteeDuty(dto *model.CommitteeDutyTrace) error {
	prefix := s.makeCommitteePrefix(dto)

	value, err := dto.MarshalSSZ()
	if err != nil {
		return fmt.Errorf("marshall ValidatorDutyTrace: %w", err)
	}

	tx := s.db.Begin()
	defer tx.Discard()

	err = s.db.Using(tx).Set(prefix, nil, value)
	if err != nil {
		return fmt.Errorf("save full ValidatorDutyTrace: %w", err)
	}

	return tx.Commit()
}

func (s *DutyTraceStore) makeValidatorSlotPrefix(role spectypes.BeaconRole, slot phase0.Slot, index phase0.ValidatorIndex) []byte {
	prefix := make([]byte, 0, len(validatorDutyTraceKey)+1+4)
	prefix = append(prefix, []byte(validatorDutyTraceKey)...)
	prefix = append(prefix, byte(role&0xff))
	prefix = append(prefix, slotToByteSlice(slot)...)
	prefix = append(prefix, uInt64ToByteSlice(uint64(index))...)
	return prefix
}

func (s *DutyTraceStore) makeValidatorPrefix(dto *model.ValidatorDutyTrace) []byte {
	prefix := make([]byte, 0, len(validatorDutyTraceKey)+4+1+8)
	prefix = append(prefix, []byte(validatorDutyTraceKey)...)
	prefix = append(prefix, byte(dto.Role&0xff))
	prefix = append(prefix, slotToByteSlice(dto.Slot)...)
	prefix = append(prefix, uInt64ToByteSlice(uint64(dto.Validator))...)
	return prefix
}

func (s *DutyTraceStore) makeCommitteePrefix(dto *model.CommitteeDutyTrace) []byte {
	prefix := make([]byte, 0, len(commiteeDutyTraceKey)+4+1+8)
	prefix = append(prefix, []byte(commiteeDutyTraceKey)...)
	prefix = append(prefix, slotToByteSlice(dto.Slot)...)
	prefix = append(prefix, dto.CommitteeID[:]...)
	return prefix
}

func (s *DutyTraceStore) makeCommiteeValidatorIndexPrefixes(ii []phase0.ValidatorIndex, slot phase0.Slot) (keys [][]byte) {
	for _, index := range ii {
		prefix := make([]byte, 0, len(commiteeVIndexKey)+1+8)
		prefix = append(prefix, []byte(commiteeVIndexKey)...)
		prefix = append(prefix, slotToByteSlice(slot)...)
		prefix = append(prefix, uInt64ToByteSlice(uint64(index))...)
		keys = append(keys, prefix)
	}

	return
}

// helpers

func slotToByteSlice(v phase0.Slot) []byte {
	b := make([]byte, 4)
	// we're casting down but we should be good for now
	// #nosec G115
	slot := uint32(uint64(v))
	binary.LittleEndian.PutUint32(b, slot)
	return b
}

func uInt64ToByteSlice(n uint64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, n)
	return b
}
