package store

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	model "github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/storage/basedb"
)

var ErrNotFound = errors.New("duty not found")

const (
	validatorDutyTraceKey      = "vd"
	committeeDutyTraceKey      = "cd"
	validatorCommitteeIndexKey = "vc"
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
	prefix := s.makeValidatorPrefix(slot, role, index)

	value, err := dto.MarshalSSZ()
	if err != nil {
		return fmt.Errorf("marshall validator duty: %w", err)
	}

	if err = s.db.Set(prefix, nil, value); err != nil {
		return fmt.Errorf("save validator duty: %w", err)
	}

	return nil
}

func (s *DutyTraceStore) SaveValidatorDuties(duties []*model.ValidatorDutyTrace) error {
	return s.db.SetMany(nil, len(duties), func(i int) (basedb.Obj, error) {
		value, err := duties[i].MarshalSSZ()
		if err != nil {
			return basedb.Obj{}, fmt.Errorf("marshall committee duty: %w", err)
		}
		role := duties[i].Role
		slot := duties[i].Slot
		index := duties[i].Validator

		key := s.makeValidatorPrefix(slot, role, index)
		return basedb.Obj{
			Key:   key,
			Value: value,
		}, nil
	})
}

func (s *DutyTraceStore) GetValidatorDuty(slot phase0.Slot, role spectypes.BeaconRole, index phase0.ValidatorIndex) (*model.ValidatorDutyTrace, error) {
	prefix := s.makeValidatorPrefix(slot, role, index)
	obj, found, err := s.db.Get(prefix, nil)
	if err != nil {
		return nil, fmt.Errorf("get validator duty: %w", err)
	}
	if !found {
		return nil, ErrNotFound
	}

	duty := new(model.ValidatorDutyTrace)
	if err := duty.UnmarshalSSZ(obj.Value); err != nil {
		return nil, fmt.Errorf("unmarshall validator duty: %w", err)
	}

	return duty, nil
}

func (s *DutyTraceStore) GetValidatorDuties(role spectypes.BeaconRole, slot phase0.Slot) (duties []*model.ValidatorDutyTrace, errs []error) {
	prefix := s.makeValidatorPrefix(slot, role)
	scanErr := s.db.GetAll(prefix, func(_ int, obj basedb.Obj) error {
		duty := new(model.ValidatorDutyTrace)
		if err := duty.UnmarshalSSZ(obj.Value); err != nil {
			errs = append(errs, fmt.Errorf("unmarshall validator duty: %w", err))
			return nil
		}
		duties = append(duties, duty)
		return nil
	})
	if scanErr != nil {
		errs = append(errs, scanErr)
	}

	return duties, errs
}

func (s *DutyTraceStore) GetCommitteeDutyLink(slot phase0.Slot, index phase0.ValidatorIndex) (id spectypes.CommitteeID, err error) {
	prefix := s.makeValidatorCommitteePrefix(slot)
	key := uInt64ToByteSlice(uint64(index))
	obj, found, err := s.db.Get(prefix, key)
	if err != nil {
		return spectypes.CommitteeID{}, fmt.Errorf("get committee duty link: %w", err)
	}
	if !found {
		return spectypes.CommitteeID{}, ErrNotFound
	}

	return spectypes.CommitteeID(obj.Value), nil
}

func (s *DutyTraceStore) GetCommitteeDutyLinks(slot phase0.Slot) (links []*model.CommitteeDutyLink, err error) {
	prefix := s.makeValidatorCommitteePrefix(slot)
	err = s.db.GetAll(prefix, func(_ int, obj basedb.Obj) error {
		var committeeID spectypes.CommitteeID
		copy(committeeID[:], obj.Value)
		index := binary.LittleEndian.Uint64(obj.Key)
		links = append(links, &model.CommitteeDutyLink{
			ValidatorIndex: phase0.ValidatorIndex(index),
			CommitteeID:    committeeID,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	return links, nil
}

func (s *DutyTraceStore) SaveCommitteeDutyLink(slot phase0.Slot, index phase0.ValidatorIndex, id spectypes.CommitteeID) error {
	prefix := s.makeValidatorCommitteePrefix(slot)
	key := uInt64ToByteSlice(uint64(index))
	if err := s.db.Set(prefix, key, id[:]); err != nil {
		return fmt.Errorf("save committee duty link: %w", err)
	}

	return nil
}

type link struct {
	Index phase0.ValidatorIndex
	ID    spectypes.CommitteeID
}

func (s *DutyTraceStore) SaveCommitteeDutyLinks(slot phase0.Slot, linkMap map[phase0.ValidatorIndex]spectypes.CommitteeID) error {
	prefix := s.makeValidatorCommitteePrefix(slot)

	var links = make([]link, 0, len(linkMap))
	for index, id := range linkMap {
		links = append(links, link{
			Index: index,
			ID:    id,
		})
	}

	return s.db.SetMany(prefix, len(links), func(i int) (basedb.Obj, error) {
		return basedb.Obj{
			Key:   uInt64ToByteSlice(uint64(links[i].Index)),
			Value: links[i].ID[:],
		}, nil
	})
}

func (s *DutyTraceStore) SaveCommitteeDuties(slot phase0.Slot, duties []*model.CommitteeDutyTrace) error {
	prefix := s.makeCommitteeSlotPrefix(slot)

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

func (s *DutyTraceStore) SaveCommitteeDuty(duty *model.CommitteeDutyTrace) error {
	prefix := s.makeCommitteePrefix(duty.Slot, duty.CommitteeID)

	value, err := duty.MarshalSSZ()
	if err != nil {
		return fmt.Errorf("marshall committee duty: %w", err)
	}

	if err = s.db.Set(prefix, nil, value); err != nil {
		return fmt.Errorf("save committee duty: %w", err)
	}

	return nil
}

func (s *DutyTraceStore) GetCommitteeDuties(slot phase0.Slot) ([]*model.CommitteeDutyTrace, []error) {
	prefix := s.makeCommitteeSlotPrefix(slot)

	var duties []*model.CommitteeDutyTrace
	var errs []error
	err := s.db.GetAll(prefix, func(i int, obj basedb.Obj) error {
		duty := new(model.CommitteeDutyTrace)
		if err := duty.UnmarshalSSZ(obj.Value); err != nil {
			errs = append(errs, fmt.Errorf("unmarshall committee duty: %w", err))
			return nil
		}
		duties = append(duties, duty)
		return nil
	})

	if err != nil {
		errs = append(errs, fmt.Errorf("get all committee IDs: %w", err))
	}

	return duties, errs
}

func (s *DutyTraceStore) GetCommitteeDuty(slot phase0.Slot, committeeID spectypes.CommitteeID) (duty *model.CommitteeDutyTrace, err error) {
	prefix := s.makeCommitteePrefix(slot, committeeID)
	obj, found, err := s.db.Get(prefix, nil)
	if err != nil {
		return nil, fmt.Errorf("get committee duty: %w", err)
	}
	if !found {
		return nil, ErrNotFound
	}

	duty = new(model.CommitteeDutyTrace)
	if err := duty.UnmarshalSSZ(obj.Value); err != nil {
		return nil, fmt.Errorf("unmarshall committee duty: %w", err)
	}

	return
}

func (s *DutyTraceStore) makeValidatorPrefix(slot phase0.Slot, role spectypes.BeaconRole, index ...phase0.ValidatorIndex) []byte {
	prefix := make([]byte, 0, len(validatorDutyTraceKey)+4+1)
	prefix = append(prefix, []byte(validatorDutyTraceKey)...)
	prefix = append(prefix, slotToByteSlice(slot)...)
	prefix = append(prefix, byte(role&0xff))
	if len(index) > 0 { // optional
		prefix = append(prefix, uInt64ToByteSlice(uint64(index[0]))...)
	}
	return prefix
}

func (s *DutyTraceStore) makeCommitteeSlotPrefix(slot phase0.Slot) []byte {
	prefix := make([]byte, 0, len(committeeDutyTraceKey)+4)
	prefix = append(prefix, []byte(committeeDutyTraceKey)...)
	prefix = append(prefix, slotToByteSlice(slot)...)
	return prefix
}

func (s *DutyTraceStore) makeCommitteePrefix(slot phase0.Slot, id spectypes.CommitteeID) []byte {
	prefix := make([]byte, 0, len(committeeDutyTraceKey)+4+32)
	prefix = append(prefix, []byte(committeeDutyTraceKey)...)
	prefix = append(prefix, slotToByteSlice(slot)...)
	prefix = append(prefix, id[:]...)
	return prefix
}

func (s *DutyTraceStore) makeValidatorCommitteePrefix(slot phase0.Slot) []byte {
	prefix := make([]byte, 0, len(validatorCommitteeIndexKey)+4)
	prefix = append(prefix, []byte(validatorCommitteeIndexKey)...)
	return append(prefix, slotToByteSlice(slot)...)
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
