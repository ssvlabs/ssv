package validator

import (
	"fmt"
	"slices"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/exporter/rolemask"
	"github.com/ssvlabs/ssv/utils/hashmap"
)

// ParticipantsRangeIndexEntry mirrors qbftstorage.ParticipantsRangeEntry but uses a validator index
// instead of a pubkey for internal representation. API layers can map the index back to pubkey.
type ParticipantsRangeIndexEntry struct {
	Slot    phase0.Slot
	Index   phase0.ValidatorIndex
	Signers []spectypes.OperatorID
}

var ErrNotFound = errors.New("not found")

// implemented by DutyStoreMetrics
type DutyTraceStore interface {
	SaveCommitteeDutyLink(slot phase0.Slot, index phase0.ValidatorIndex, id spectypes.CommitteeID) error
	SaveCommitteeDutyLinks(slot phase0.Slot, linkMap map[phase0.ValidatorIndex]spectypes.CommitteeID) error
	SaveCommitteeDuty(duty *exporter.CommitteeDutyTrace) error
	SaveCommitteeDuties(slot phase0.Slot, duties []*exporter.CommitteeDutyTrace) error
	SaveValidatorDuty(duty *exporter.ValidatorDutyTrace) error
	SaveValidatorDuties(duties []*exporter.ValidatorDutyTrace) error
	GetCommitteeDuty(slot phase0.Slot, committeeID spectypes.CommitteeID) (*exporter.CommitteeDutyTrace, error)
	GetCommitteeDuties(slot phase0.Slot) ([]*exporter.CommitteeDutyTrace, error)
	GetCommitteeDutyLink(slot phase0.Slot, index phase0.ValidatorIndex) (spectypes.CommitteeID, error)
	GetCommitteeDutyLinks(slot phase0.Slot) ([]*exporter.CommitteeDutyLink, error)
	GetValidatorDuty(slot phase0.Slot, role spectypes.BeaconRole, index phase0.ValidatorIndex) (*exporter.ValidatorDutyTrace, error)
	GetValidatorDuties(role spectypes.BeaconRole, slot phase0.Slot) ([]*exporter.ValidatorDutyTrace, error)

	// Compact scheduled duties I/O
	SaveScheduled(slot phase0.Slot, schedule map[phase0.ValidatorIndex]rolemask.Mask) error
	GetScheduled(slot phase0.Slot) (map[phase0.ValidatorIndex]rolemask.Mask, error)
}

func (c *Collector) GetCommitteeID(slot phase0.Slot, index phase0.ValidatorIndex) (spectypes.CommitteeID, error) {
	committeeID, err := c.getCommitteeIDBySlotAndIndex(slot, index)
	if err != nil {
		return spectypes.CommitteeID{}, err
	}

	return committeeID, nil
}

func (c *Collector) GetValidatorDuties(role spectypes.BeaconRole, slot phase0.Slot) ([]*exporter.ValidatorDutyTrace, error) {
	duties := []*exporter.ValidatorDutyTrace{}
	var errs *multierror.Error

	// lookup in cache
	c.validatorTraces.Range(func(_ phase0.ValidatorIndex, validatorSlots *hashmap.Map[phase0.Slot, *validatorDutyTrace]) bool {
		traces, found := validatorSlots.Get(slot)
		if found {
			traces.Lock()
			defer traces.Unlock()

			// find the trace for the role
			for _, trace := range traces.roles {
				if trace.Role == role {
					duties = append(duties, trace.DeepCopy())
				}
			}
		}
		return true // keep iterating
	})

	// go to disk for the older ones
	storeDuties, err := c.getValidatorDutiesFromDisk(role, slot)
	duties = append(duties, storeDuties...)
	errs = multierror.Append(errs, err)

	return duties, errs.ErrorOrNil()
}

func (c *Collector) getValidatorDutiesFromDisk(role spectypes.BeaconRole, slot phase0.Slot) ([]*exporter.ValidatorDutyTrace, error) {
	duties := []*exporter.ValidatorDutyTrace{}
	var errs *multierror.Error

	storeDuties, err := c.store.GetValidatorDuties(role, slot)
	errs = multierror.Append(errs, err)

	for _, duty := range storeDuties {
		duties = append(duties, duty.DeepCopy())
	}
	return duties, errs.ErrorOrNil()
}

func (c *Collector) GetValidatorDuty(role spectypes.BeaconRole, slot phase0.Slot, index phase0.ValidatorIndex) (*exporter.ValidatorDutyTrace, error) {
	// lookup in cache
	validatorSlots, found := c.validatorTraces.Get(index)
	if !found {
		return c.getValidatorDutyFromDiskIndex(role, slot, index)
	}

	traces, found := validatorSlots.Get(slot)
	if found {
		traces.Lock()
		defer traces.Unlock()

		// find the trace for the role
		for _, trace := range traces.roles {
			if trace.Role == role {
				return trace.DeepCopy(), nil
			}
		}
	}

	// go to disk for the older ones
	return c.getValidatorDutyFromDiskIndex(role, slot, index)
}

func (c *Collector) getValidatorDutyFromDiskIndex(role spectypes.BeaconRole, slot phase0.Slot, index phase0.ValidatorIndex) (*exporter.ValidatorDutyTrace, error) {
	trace, err := c.store.GetValidatorDuty(slot, role, index)
	if err != nil {
		return nil, fmt.Errorf("get validator duty from disk (role=%s slot=%d index=%d): %w", role, slot, index, err)
	}

	return trace, nil
}

func (c *Collector) GetCommitteeDuties(wantSlot phase0.Slot, roles ...spectypes.BeaconRole) ([]*exporter.CommitteeDutyTrace, error) {
	var duties []*exporter.CommitteeDutyTrace
	var errs *multierror.Error

	c.committeeTraces.Range(func(committeeID spectypes.CommitteeID, committeeSlots *hashmap.Map[phase0.Slot, *committeeDutyTrace]) bool {
		dt, found := committeeSlots.Get(wantSlot)
		if found {
			duties = append(duties, dt.trace())
		}
		return true // keep iterating
	})

	diskDuties, err := c.store.GetCommitteeDuties(wantSlot)
	duties = append(duties, diskDuties...)
	errs = multierror.Append(errs, err)

	var filteredDuties []*exporter.CommitteeDutyTrace
	for _, duty := range duties {
		if hasSignersForRoles(duty, roles...) {
			filteredDuties = append(filteredDuties, duty)
		}
	}

	return filteredDuties, errs.ErrorOrNil()
}

func (c *Collector) GetCommitteeDuty(slot phase0.Slot, committeeID spectypes.CommitteeID, roles ...spectypes.BeaconRole) (*exporter.CommitteeDutyTrace, error) {
	committeeSlots, found := c.committeeTraces.Get(committeeID)
	if !found {
		trace, err := c.getCommitteeDutyFromDisk(slot, committeeID)
		if err != nil {
			return nil, err
		}

		if hasSignersForRoles(trace, roles...) {
			return trace, nil
		}

		return nil, ErrNotFound
	}

	trace, found := committeeSlots.Get(slot)
	if !found {
		trace, err := c.getCommitteeDutyFromDisk(slot, committeeID)
		if err != nil {
			return nil, err
		}

		if hasSignersForRoles(trace, roles...) {
			return trace, nil
		}

		return nil, ErrNotFound
	}

	clone := trace.trace()

	if !hasSignersForRoles(clone, roles...) {
		return nil, ErrNotFound
	}

	return clone, nil
}

// hasSignersForRoles checks if the duty has signers for the given role
// since we don't store a boolean flag to separate duties by their role in the db
// we rely on the fact that during collection we separate the signers in their
// corresponding fields (Attester and SyncCommittee) based on the role
func hasSignersForRoles(duty *exporter.CommitteeDutyTrace, roles ...spectypes.BeaconRole) bool {
	if len(roles) == 0 {
		return true
	}
	for _, role := range roles {
		if role == spectypes.BNRoleAttester {
			if len(duty.Attester) == 0 {
				return false
			}
		}
		if role == spectypes.BNRoleSyncCommittee {
			if len(duty.SyncCommittee) == 0 {
				return false
			}
		}
	}
	return true
}

func (c *Collector) getCommitteeDutyFromDisk(slot phase0.Slot, committeeID spectypes.CommitteeID) (*exporter.CommitteeDutyTrace, error) {
	ctx := fmt.Sprintf("slot=%d committeeID=%x", slot, committeeID)
	trace, err := c.store.GetCommitteeDuty(slot, committeeID)
	if err != nil {
		return nil, fmt.Errorf("get committee duty from disk (%s): %w", ctx, err)
	}

	return trace, nil
}

func (c *Collector) GetAllCommitteeDecideds(slot phase0.Slot, roles ...spectypes.BeaconRole) ([]ParticipantsRangeIndexEntry, error) {
	var errs *multierror.Error

	duties, err := c.GetCommitteeDuties(slot, roles...)
	errs = multierror.Append(errs, err)
	if len(duties) == 0 {
		return nil, errs.ErrorOrNil()
	}

	out := make([]ParticipantsRangeIndexEntry, 0, len(duties))

	links, err := c.GetCommitteeDutyLinks(slot)
	errs = multierror.Append(errs, err)

	mapping := make(map[spectypes.CommitteeID]phase0.ValidatorIndex)
	for _, link := range links {
		if _, exists := mapping[link.CommitteeID]; !exists {
			mapping[link.CommitteeID] = link.ValidatorIndex
		}
	}

	for _, duty := range duties {
		signers := make([]spectypes.OperatorID, 0, len(duty.Decideds)+len(duty.SyncCommittee)+len(duty.Attester))
		for _, d := range duty.Decideds {
			signers = append(signers, d.Signers...)
		}

		for _, round := range duty.SyncCommittee {
			signers = append(signers, round.Signer)
		}

		for _, round := range duty.Attester {
			signers = append(signers, round.Signer)
		}

		slices.Sort(signers)
		signers = slices.Compact(signers)

		out = append(out, ParticipantsRangeIndexEntry{
			Slot:    slot,
			Index:   mapping[duty.CommitteeID],
			Signers: signers,
		})
	}

	return out, errs.ErrorOrNil()
}

func (c *Collector) GetCommitteeDutyLinks(slot phase0.Slot) ([]*exporter.CommitteeDutyLink, error) {
	out := make([]*exporter.CommitteeDutyLink, 0)
	var errs *multierror.Error

	c.validatorIndexToCommitteeLinks.Range(func(vi phase0.ValidatorIndex, m *hashmap.Map[phase0.Slot, spectypes.CommitteeID]) bool {
		cid, found := m.Get(slot)
		if found {
			out = append(out, &exporter.CommitteeDutyLink{
				ValidatorIndex: vi,
				CommitteeID:    cid,
			})
		}
		return true
	})

	links, err := c.store.GetCommitteeDutyLinks(slot)
	out = append(out, links...)
	errs = multierror.Append(errs, err)

	return out, errs.ErrorOrNil()
}

func (c *Collector) GetCommitteeDecideds(slot phase0.Slot, index phase0.ValidatorIndex, roles ...spectypes.BeaconRole) (out []ParticipantsRangeIndexEntry, err error) {
	committeeID, err := c.getCommitteeIDBySlotAndIndex(slot, index)
	if err != nil {
		return nil, fmt.Errorf("get committee ID by slot(%d) and index(%d): %w", slot, index, err)
	}

	duty, err := c.GetCommitteeDuty(slot, committeeID, roles...)
	if err != nil {
		return nil, fmt.Errorf("get committee duty: %w", err)
	}

	signers := make([]spectypes.OperatorID, 0, len(duty.Decideds)+len(duty.SyncCommittee)+len(duty.Attester))

	for _, d := range duty.Decideds {
		signers = append(signers, d.Signers...)
	}

	for _, round := range duty.SyncCommittee {
		signers = append(signers, round.Signer)
	}

	for _, round := range duty.Attester {
		signers = append(signers, round.Signer)
	}

	slices.Sort(signers)
	signers = slices.Compact(signers)

	out = append(out, ParticipantsRangeIndexEntry{
		Slot:    slot,
		Index:   index,
		Signers: signers,
	})

	return out, nil
}

func (c *Collector) GetValidatorDecideds(role spectypes.BeaconRole, slot phase0.Slot, indices []phase0.ValidatorIndex) ([]ParticipantsRangeIndexEntry, error) {
	out := make([]ParticipantsRangeIndexEntry, 0, len(indices))
	var errs *multierror.Error

	for _, index := range indices {
		duty, err := c.GetValidatorDuty(role, slot, index)
		if err != nil {
			errs = multierror.Append(errs, err)
			continue
		}

		signers := make([]spectypes.OperatorID, 0, len(duty.Decideds)+len(duty.Post))

		for _, d := range duty.Decideds {
			signers = append(signers, d.Signers...)
		}

		for _, post := range duty.Post {
			signers = append(signers, post.Signer)
		}

		slices.Sort(signers)
		signers = slices.Compact(signers)

		out = append(out, ParticipantsRangeIndexEntry{
			Slot:    slot,
			Index:   index,
			Signers: signers,
		})
	}

	return out, errs.ErrorOrNil()
}

func (c *Collector) GetAllValidatorDecideds(role spectypes.BeaconRole, slot phase0.Slot) ([]ParticipantsRangeIndexEntry, error) {
	var errs *multierror.Error

	duties, err := c.store.GetValidatorDuties(role, slot)
	errs = multierror.Append(errs, err)

	out := make([]ParticipantsRangeIndexEntry, 0, len(duties))

	for _, duty := range duties {
		signers := make([]spectypes.OperatorID, 0, len(duty.Decideds)+len(duty.Post))

		for _, d := range duty.Decideds {
			signers = append(signers, d.Signers...)
		}

		for _, post := range duty.Post {
			signers = append(signers, post.Signer)
		}

		slices.Sort(signers)
		signers = slices.Compact(signers)

		out = append(out, ParticipantsRangeIndexEntry{
			Slot:    slot,
			Index:   duty.Validator,
			Signers: signers,
		})
	}

	return out, errs.ErrorOrNil()
}

func (c *Collector) getCommitteeIDBySlotAndIndex(slot phase0.Slot, index phase0.ValidatorIndex) (spectypes.CommitteeID, error) {
	slotToCommittee, found := c.validatorIndexToCommitteeLinks.Get(index)
	if !found {
		return c.getCommitteeIDFromDisk(slot, index)
	}

	committeeID, found := slotToCommittee.Get(slot)
	if !found {
		return c.getCommitteeIDFromDisk(slot, index)
	}

	return committeeID, nil
}

func (c *Collector) getCommitteeIDFromDisk(slot phase0.Slot, index phase0.ValidatorIndex) (spectypes.CommitteeID, error) {
	ctx := fmt.Sprintf("slot=%d index=%d", slot, index)
	link, err := c.store.GetCommitteeDutyLink(slot, index)
	if err != nil {
		return spectypes.CommitteeID{}, fmt.Errorf("get committee ID from disk (%s): %w", ctx, err)
	}

	return link, nil
}
