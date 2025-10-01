package store

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/RoaringBitmap/roaring/roaring64"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/hashicorp/go-multierror"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/exporter/rolemask"
	"github.com/ssvlabs/ssv/storage/basedb"
)

var ErrNotFound = errors.New("duty not found")

const (
	validatorDutyTraceKey      = "vd"
	committeeDutyTraceKey      = "cd"
	validatorCommitteeIndexKey = "vc"
	scheduledDutyKey           = "sd"
)

type DutyTraceStore struct {
	db basedb.Database
}

func New(db basedb.Database) *DutyTraceStore {
	return &DutyTraceStore{
		db: db,
	}
}

func (s *DutyTraceStore) SaveValidatorDuty(dto *exporter.ValidatorDutyTrace) error {
	role, slot, index := dto.Role, dto.Slot, dto.Validator
	prefix := s.makeValidatorPrefix(slot, role, index)

	ctx := fmt.Sprintf("role=%s slot=%d index=%d", role, slot, index)
	value, err := dto.MarshalSSZ()
	if err != nil {
		return fmt.Errorf("marshal validator duty (%s): %w", ctx, err)
	}

	if err = s.db.Set(prefix, nil, value); err != nil {
		return fmt.Errorf("save validator duty (%s): %w", ctx, err)
	}

	return nil
}

func (s *DutyTraceStore) SaveValidatorDuties(duties []*exporter.ValidatorDutyTrace) error {
	return s.db.SetMany(nil, len(duties), func(i int) (basedb.Obj, error) {
		role := duties[i].Role
		slot := duties[i].Slot
		index := duties[i].Validator
		ctx := fmt.Sprintf("role=%s slot=%d index=%d", role, slot, index)
		value, err := duties[i].MarshalSSZ()
		if err != nil {
			return basedb.Obj{}, fmt.Errorf("marshal validator duty (%s): %w", ctx, err)
		}

		key := s.makeValidatorPrefix(slot, role, index)
		return basedb.Obj{
			Key:   key,
			Value: value,
		}, nil
	})
}

func (s *DutyTraceStore) GetValidatorDuty(slot phase0.Slot, role spectypes.BeaconRole, index phase0.ValidatorIndex) (*exporter.ValidatorDutyTrace, error) {
	prefix := s.makeValidatorPrefix(slot, role, index)
	ctx := fmt.Sprintf("role=%s slot=%d index=%d", role, slot, index)

	obj, found, err := s.db.Get(prefix, nil)
	if err != nil {
		return nil, fmt.Errorf("get validator duty (%s): %w", ctx, err)
	}
	if !found {
		return nil, fmt.Errorf("get validator duty (%s): %w", ctx, ErrNotFound)
	}

	duty := new(exporter.ValidatorDutyTrace)
	if err := duty.UnmarshalSSZ(obj.Value); err != nil {
		return nil, fmt.Errorf("unmarshal validator duty (%s): %w", ctx, err)
	}

	return duty, nil
}

func (s *DutyTraceStore) GetValidatorDuties(role spectypes.BeaconRole, slot phase0.Slot) ([]*exporter.ValidatorDutyTrace, error) {
	var duties []*exporter.ValidatorDutyTrace
	var errs *multierror.Error

	prefix := s.makeValidatorPrefix(slot, role)
	ctx := fmt.Sprintf("role=%s slot=%d", role, slot)

	iterationError := s.db.GetAll(prefix, func(_ int, obj basedb.Obj) error {
		duty := new(exporter.ValidatorDutyTrace)
		if err := duty.UnmarshalSSZ(obj.Value); err != nil {
			errs = multierror.Append(errs, fmt.Errorf("unmarshal validator duty (%s): %w", ctx, err))
		} else {
			duties = append(duties, duty)
		}
		return nil
	})
	if iterationError != nil {
		errs = multierror.Append(errs, fmt.Errorf("iterate validator duties (%s): %w", ctx, iterationError))
	}

	return duties, errs.ErrorOrNil()
}

func (s *DutyTraceStore) GetCommitteeDutyLink(slot phase0.Slot, index phase0.ValidatorIndex) (spectypes.CommitteeID, error) {
	prefix := s.makeValidatorCommitteePrefix(slot)
	ctx := fmt.Sprintf("slot=%d index=%d", slot, index)

	key := uInt64ToByteSlice(uint64(index))
	obj, found, err := s.db.Get(prefix, key)
	if err != nil {
		return spectypes.CommitteeID{}, fmt.Errorf("get committee duty link (%s): %w", ctx, err)
	}
	if !found {
		return spectypes.CommitteeID{}, fmt.Errorf("get committee duty link (%s): %w", ctx, ErrNotFound)
	}
	return spectypes.CommitteeID(obj.Value), nil
}

func (s *DutyTraceStore) GetCommitteeDutyLinks(slot phase0.Slot) ([]*exporter.CommitteeDutyLink, error) {
	var links []*exporter.CommitteeDutyLink
	var errs *multierror.Error

	prefix := s.makeValidatorCommitteePrefix(slot)
	ctx := fmt.Sprintf("slot=%d", slot)

	iterationError := s.db.GetAll(prefix, func(_ int, obj basedb.Obj) error {
		var committeeID spectypes.CommitteeID
		copy(committeeID[:], obj.Value)
		index := binary.LittleEndian.Uint64(obj.Key)
		links = append(links, &exporter.CommitteeDutyLink{
			ValidatorIndex: phase0.ValidatorIndex(index),
			CommitteeID:    committeeID,
		})
		return nil
	})
	if iterationError != nil {
		errs = multierror.Append(errs, fmt.Errorf("iterate committee duty links (%s): %w", ctx, iterationError))
	}

	return links, errs.ErrorOrNil()
}

func (s *DutyTraceStore) SaveCommitteeDutyLink(slot phase0.Slot, index phase0.ValidatorIndex, id spectypes.CommitteeID) error {
	ctx := fmt.Sprintf("slot=%d index=%d committeeID=%x", slot, index, id)
	prefix := s.makeValidatorCommitteePrefix(slot)
	key := uInt64ToByteSlice(uint64(index))
	if err := s.db.Set(prefix, key, id[:]); err != nil {
		return fmt.Errorf("save committee duty link (%s): %w", ctx, err)
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

func (s *DutyTraceStore) SaveCommitteeDuties(slot phase0.Slot, duties []*exporter.CommitteeDutyTrace) error {
	prefix := s.makeCommitteeSlotPrefix(slot)

	return s.db.SetMany(prefix, len(duties), func(i int) (basedb.Obj, error) {
		ctx := fmt.Sprintf("slot=%d committeeID=%x", duties[i].Slot, duties[i].CommitteeID)
		value, err := duties[i].MarshalSSZ()
		if err != nil {
			return basedb.Obj{}, fmt.Errorf("marshal committee duty (%s): %w", ctx, err)
		}
		return basedb.Obj{
			Value: value,
			Key:   duties[i].CommitteeID[:],
		}, nil
	})
}

func (s *DutyTraceStore) SaveCommitteeDuty(duty *exporter.CommitteeDutyTrace) error {
	prefix := s.makeCommitteePrefix(duty.Slot, duty.CommitteeID)

	ctx := fmt.Sprintf("slot=%d committeeID=%x", duty.Slot, duty.CommitteeID)
	value, err := duty.MarshalSSZ()
	if err != nil {
		return fmt.Errorf("marshal committee duty (%s): %w", ctx, err)
	}

	if err = s.db.Set(prefix, nil, value); err != nil {
		return fmt.Errorf("save committee duty (%s): %w", ctx, err)
	}

	return nil
}

func (s *DutyTraceStore) GetCommitteeDuties(slot phase0.Slot) ([]*exporter.CommitteeDutyTrace, error) {
	var duties []*exporter.CommitteeDutyTrace
	var errs *multierror.Error

	prefix := s.makeCommitteeSlotPrefix(slot)
	ctx := fmt.Sprintf("slot=%d", slot)

	iterationError := s.db.GetAll(prefix, func(i int, obj basedb.Obj) error {
		duty := new(exporter.CommitteeDutyTrace)
		if err := duty.UnmarshalSSZ(obj.Value); err != nil {
			errs = multierror.Append(errs, fmt.Errorf("unmarshal committee duty (%s): %w", ctx, err))
		} else {
			duties = append(duties, duty)
		}
		return nil
	})
	if iterationError != nil {
		errs = multierror.Append(errs, fmt.Errorf("iterate committee duties (%s): %w", ctx, iterationError))
	}

	return duties, errs.ErrorOrNil()
}

func (s *DutyTraceStore) GetCommitteeDuty(slot phase0.Slot, committeeID spectypes.CommitteeID) (duty *exporter.CommitteeDutyTrace, err error) {
	prefix := s.makeCommitteePrefix(slot, committeeID)
	ctx := fmt.Sprintf("slot=%d committeeID=%x", slot, committeeID)

	obj, found, err := s.db.Get(prefix, nil)
	if err != nil {
		return nil, fmt.Errorf("get committee duty (%s): %w", ctx, err)
	}
	if !found {
		return nil, fmt.Errorf("get committee duty (%s): %w", ctx, ErrNotFound)
	}

	duty = new(exporter.CommitteeDutyTrace)
	if err := duty.UnmarshalSSZ(obj.Value); err != nil {
		return nil, fmt.Errorf("unmarshal committee duty (%s): %w", ctx, err)
	}

	return
}

// Scheduled duties: per (slot, role) roaring64 bitmap of validator indices

// SetScheduledRole overwrites scheduled indices for a (slot, role).
func (s *DutyTraceStore) SetScheduledRole(slot phase0.Slot, role spectypes.BeaconRole, indices []phase0.ValidatorIndex) error {
	prefix := s.makeScheduledRolePrefix(slot, role)
	if len(indices) == 0 {
		return s.db.Delete(prefix, nil)
	}
	bm := roaring64.NewBitmap()
	for _, idx := range indices {
		bm.Add(uint64(idx))
	}
	var buf bytes.Buffer
	if _, err := bm.WriteTo(&buf); err != nil {
		return fmt.Errorf("set scheduled (slot=%d role=%d): serialize: %w", slot, role, err)
	}
	if err := s.db.Set(prefix, nil, buf.Bytes()); err != nil {
		return fmt.Errorf("set scheduled (slot=%d role=%d): %w", slot, role, err)
	}
	return nil
}

// AddScheduledRole unions indices into existing (slot, role) bitmap.
func (s *DutyTraceStore) AddScheduledRole(slot phase0.Slot, role spectypes.BeaconRole, indices []phase0.ValidatorIndex) error {
	if len(indices) == 0 {
		return nil
	}
	prefix := s.makeScheduledRolePrefix(slot, role)
	bm := roaring64.NewBitmap()
	for _, idx := range indices {
		bm.Add(uint64(idx))
	}
	if existing, found, err := s.db.Get(prefix, nil); err != nil {
		return fmt.Errorf("add scheduled (slot=%d role=%d): get existing: %w", slot, role, err)
	} else if found {
		var prev roaring64.Bitmap
		if _, err := prev.ReadFrom(bytes.NewReader(existing.Value)); err != nil {
			return fmt.Errorf("add scheduled (slot=%d role=%d): read existing: %w", slot, role, err)
		}
		bm.Or(&prev)
	}
	var buf bytes.Buffer
	if _, err := bm.WriteTo(&buf); err != nil {
		return fmt.Errorf("add scheduled (slot=%d role=%d): serialize: %w", slot, role, err)
	}
	if err := s.db.Set(prefix, nil, buf.Bytes()); err != nil {
		return fmt.Errorf("add scheduled (slot=%d role=%d): %w", slot, role, err)
	}
	return nil
}

// GetScheduledRole returns indices for a (slot, role).
func (s *DutyTraceStore) GetScheduledRole(slot phase0.Slot, role spectypes.BeaconRole) ([]phase0.ValidatorIndex, error) {
	prefix := s.makeScheduledRolePrefix(slot, role)
	obj, found, err := s.db.Get(prefix, nil)
	if err != nil {
		return nil, fmt.Errorf("get scheduled role (slot=%d role=%d): %w", slot, role, err)
	}
	if !found {
		return nil, ErrNotFound
	}
	var bm roaring64.Bitmap
	if _, err := bm.ReadFrom(bytes.NewReader(obj.Value)); err != nil {
		return nil, fmt.Errorf("get scheduled role (slot=%d role=%d): decode: %w", slot, role, err)
	}
	out := make([]phase0.ValidatorIndex, 0, bm.GetCardinality())
	it := bm.Iterator()
	for it.HasNext() {
		out = append(out, phase0.ValidatorIndex(it.Next()))
	}
	return out, nil
}

// DeleteScheduledRole removes scheduled indices for a (slot, role).
func (s *DutyTraceStore) DeleteScheduledRole(slot phase0.Slot, role spectypes.BeaconRole) error {
	prefix := s.makeScheduledRolePrefix(slot, role)
	return s.db.Delete(prefix, nil)
}

// DeleteScheduledSlot removes scheduled data for all roles at a slot.
func (s *DutyTraceStore) DeleteScheduledSlot(slot phase0.Slot) error {
	var errs *multierror.Error
	for _, role := range rolemask.All() {
		if err := s.db.Delete(s.makeScheduledRolePrefix(slot, role), nil); err != nil {
			errs = multierror.Append(errs, fmt.Errorf("delete scheduled (slot=%d role=%d): %w", slot, role, err))
		}
	}
	return errs.ErrorOrNil()
}

// SaveScheduled stores a compact map (validator index -> role mask) for a slot.
func (s *DutyTraceStore) SaveScheduled(slot phase0.Slot, schedule map[phase0.ValidatorIndex]uint8) error {
	if len(schedule) == 0 {
		return nil
	}
	for _, role := range rolemask.All() {
		bit, _ := rolemask.BitOf(role)
		bm := roaring64.NewBitmap()
		for idx, m := range schedule {
			if m&bit != 0 {
				bm.Add(uint64(idx))
			}
		}
		if bm.IsEmpty() {
			continue
		}
		prefix := s.makeScheduledRolePrefix(slot, role)
		if existing, found, err := s.db.Get(prefix, nil); err != nil {
			return fmt.Errorf("save scheduled (slot=%d role=%d): get: %w", slot, role, err)
		} else if found {
			var prev roaring64.Bitmap
			if _, err := prev.ReadFrom(bytes.NewReader(existing.Value)); err != nil {
				return fmt.Errorf("save scheduled (slot=%d role=%d): read: %w", slot, role, err)
			}
			bm.Or(&prev)
		}
		var buf bytes.Buffer
		if _, err := bm.WriteTo(&buf); err != nil {
			return fmt.Errorf("save scheduled (slot=%d role=%d): serialize: %w", slot, role, err)
		}
		if err := s.db.Set(prefix, nil, buf.Bytes()); err != nil {
			return fmt.Errorf("save scheduled (slot=%d role=%d): %w", slot, role, err)
		}
	}
	return nil
}

// GetScheduled returns compact schedule map for a slot.
func (s *DutyTraceStore) GetScheduled(slot phase0.Slot) (map[phase0.ValidatorIndex]uint8, error) {
	out := make(map[phase0.ValidatorIndex]uint8)
	var errs *multierror.Error
	for _, role := range rolemask.All() {
		bit, _ := rolemask.BitOf(role)
		prefix := s.makeScheduledRolePrefix(slot, role)
		obj, found, err := s.db.Get(prefix, nil)
		if err != nil {
			errs = multierror.Append(errs, fmt.Errorf("get scheduled (slot=%d role=%d): %w", slot, role, err))
			continue
		}
		if !found {
			continue
		}
		var bm roaring64.Bitmap
		if _, err := bm.ReadFrom(bytes.NewReader(obj.Value)); err != nil {
			errs = multierror.Append(errs, fmt.Errorf("get scheduled (slot=%d role=%d): decode: %w", slot, role, err))
			continue
		}
		it := bm.Iterator()
		for it.HasNext() {
			idx := phase0.ValidatorIndex(it.Next())
			out[idx] |= bit
		}
	}
	return out, errs.ErrorOrNil()
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

func (s *DutyTraceStore) makeScheduledRolePrefix(slot phase0.Slot, role spectypes.BeaconRole) []byte {
	prefix := make([]byte, 0, len(scheduledDutyKey)+4+1)
	prefix = append(prefix, []byte(scheduledDutyKey)...)
	prefix = append(prefix, slotToByteSlice(slot)...)
	prefix = append(prefix, byte(role&0xff))
	return prefix
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
