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
	validatorDutyTraceKey    = "vd"
	commiteeDutyTraceKey     = "cd"
	commiteeOperatorIndexKey = "ci"
)

type DutyTraceStore struct {
	db basedb.Database
}

func New(db basedb.Database) *DutyTraceStore {
	return &DutyTraceStore{
		db: db,
	}
}

func (s *DutyTraceStore) SaveValidatorDuty(dto *model.ValidatorDutyTrace) error {
	role, slot, index := dto.Role, dto.Slot, dto.Validator
	prefix := s.makeValidatorPrefix(role, slot, index)

	value, err := dto.MarshalSSZ()
	if err != nil {
		return fmt.Errorf("marshall validator duty: %w", err)
	}

	tx := s.db.Begin()
	defer tx.Discard()

	if err = s.db.Using(tx).Set(prefix, nil, value); err != nil {
		return fmt.Errorf("save validator duty: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func (s *DutyTraceStore) GetValidatorDuty(role spectypes.BeaconRole, slot phase0.Slot, index phase0.ValidatorIndex) (duty *model.ValidatorDutyTrace, err error) {
	prefix := s.makeValidatorPrefix(role, slot, index)
	obj, found, err := s.db.Get(prefix, nil)
	if err != nil {
		return nil, fmt.Errorf("get validator duty: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("validator duty not found")
	}

	duty = new(model.ValidatorDutyTrace)
	if err := duty.UnmarshalSSZ(obj.Value); err != nil {
		return nil, fmt.Errorf("unmarshall validator duty: %w", err)
	}

	return duty, nil
}

func (s *DutyTraceStore) GetAllValidatorDuties(role spectypes.BeaconRole, slot phase0.Slot) (duties []*model.ValidatorDutyTrace, err error) {
	prefix := s.makeValidatorPrefix(role, slot)
	err = s.db.GetAll(prefix, func(_ int, obj basedb.Obj) error {
		duty := new(model.ValidatorDutyTrace)
		if err := duty.UnmarshalSSZ(obj.Value); err != nil {
			return fmt.Errorf("unmarshall validator duty: %w", err)
		}
		duties = append(duties, duty)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return
}

func (s *DutyTraceStore) SaveCommiteeDuties(slot phase0.Slot, duties []*model.CommitteeDutyTrace) error {
	prefix := s.makeSlotPrefix(slot)

	return s.db.SetMany(prefix, len(duties), func(i int) (basedb.Obj, error) {
		value, err := duties[i].MarshalSSZ()
		if err != nil {
			return basedb.Obj{}, fmt.Errorf("marshall committee duty: %w", err)
		}
		return basedb.Obj{
			Value: value,
			Key:   duties[i].CommitteeID[:],
		}, nil
	})
}

func (s *DutyTraceStore) SaveCommiteeDuty(duty *model.CommitteeDutyTrace) error {
	prefix := s.makeCommitteePrefix(duty.Slot, duty.CommitteeID)

	value, err := duty.MarshalSSZ()
	if err != nil {
		return fmt.Errorf("marshall committee duty: %w", err)
	}

	tx := s.db.Begin()
	defer tx.Discard()

	if err = s.db.Using(tx).Set(prefix, nil, value); err != nil {
		return fmt.Errorf("save committee duty: %w", err)
	}

	prefixes := s.makeCommiteeOperatorPrefixes(duty.OperatorIDs, duty.Slot)

	for _, ref := range prefixes {
		if err = s.db.Using(tx).Set(ref, nil, prefix); err != nil {
			return fmt.Errorf("save committee duty index: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func (s *DutyTraceStore) GetCommitteeDuty(slot phase0.Slot, committeeID spectypes.CommitteeID) (duty *model.CommitteeDutyTrace, err error) {
	prefix := s.makeCommitteePrefix(slot, committeeID)
	obj, found, err := s.db.Get(prefix, nil)
	if err != nil {
		return nil, fmt.Errorf("get committee duty: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("committee duty not found")
	}

	duty = new(model.CommitteeDutyTrace)
	if err := duty.UnmarshalSSZ(obj.Value); err != nil {
		return nil, fmt.Errorf("unmarshall committee duty: %w", err)
	}

	return
}

func (s *DutyTraceStore) GetCommitteeDutiesByOperator(indexes []spectypes.OperatorID, slot phase0.Slot) (out []*model.CommitteeDutyTrace, err error) {
	prefixes := s.makeCommiteeOperatorPrefixes(indexes, slot)
	keys := make([][]byte, 0)

	tx := s.db.BeginRead()
	defer tx.Discard()

	for _, prefix := range prefixes {
		obj, found, err := s.db.Get(prefix, nil)
		if err != nil {
			return nil, fmt.Errorf("get committee duty index: %w", err)
		}
		if !found {
			return nil, fmt.Errorf("committee duty index not found")
		}
		keys = append(keys, obj.Value)
	}

	err = s.db.GetMany(nil, keys, func(obj basedb.Obj) error {
		duty := new(model.CommitteeDutyTrace)
		if err := duty.UnmarshalSSZ(obj.Value); err != nil {
			return fmt.Errorf("unmarshall committee duty: %w", err)
		}
		out = append(out, duty)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return
}

// role + slot + ?index
func (s *DutyTraceStore) makeValidatorPrefix(role spectypes.BeaconRole, slot phase0.Slot, index ...phase0.ValidatorIndex) []byte {
	prefix := make([]byte, 0, len(validatorDutyTraceKey)+1+4)
	prefix = append(prefix, []byte(validatorDutyTraceKey)...)
	prefix = append(prefix, byte(role&0xff))
	prefix = append(prefix, slotToByteSlice(slot)...)
	if len(index) > 0 { // optional
		prefix = append(prefix, uInt64ToByteSlice(uint64(index[0]))...)
	}
	return prefix
}

// slot only
func (s *DutyTraceStore) makeSlotPrefix(slot phase0.Slot) []byte {
	prefix := make([]byte, 0, len(commiteeDutyTraceKey)+4)
	prefix = append(prefix, []byte(commiteeDutyTraceKey)...)
	prefix = append(prefix, slotToByteSlice(slot)...)
	return prefix
}

// slot + role
func (s *DutyTraceStore) makeCommitteePrefix(slot phase0.Slot, id spectypes.CommitteeID) []byte {
	prefix := make([]byte, 0, len(commiteeDutyTraceKey)+4+32)
	prefix = append(prefix, []byte(commiteeDutyTraceKey)...)
	prefix = append(prefix, slotToByteSlice(slot)...)
	prefix = append(prefix, id[:]...)
	return prefix
}

// slot + index
func (s *DutyTraceStore) makeCommiteeOperatorPrefixes(ii []spectypes.OperatorID, slot phase0.Slot) (keys [][]byte) {
	for _, index := range ii {
		prefix := make([]byte, 0, len(commiteeOperatorIndexKey)+4+32)
		prefix = append(prefix, []byte(commiteeOperatorIndexKey)...)
		prefix = append(prefix, slotToByteSlice(slot)...)
		prefix = append(prefix, uInt64ToByteSlice(index)...)
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
