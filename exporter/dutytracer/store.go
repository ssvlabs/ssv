package dutytracer

import (
	"errors"
	"fmt"
	"slices"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/hashicorp/go-multierror"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter/rolemask"
	"github.com/ssvlabs/ssv/exporter/traces"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
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
	SaveCommitteeDuty(role spectypes.RunnerRole, duty *traces.CommitteeDutyTrace) error
	SaveCommitteeDuties(slot phase0.Slot, role spectypes.RunnerRole, duties []*traces.CommitteeDutyTrace) error
	SaveValidatorDuty(duty *traces.ValidatorDutyTrace) error
	SaveValidatorDuties(duties []*traces.ValidatorDutyTrace) error
	GetCommitteeDuty(slot phase0.Slot, role spectypes.RunnerRole, committeeID spectypes.CommitteeID) (*traces.CommitteeDutyTrace, error)
	GetCommitteeDuties(slot phase0.Slot, roles ...spectypes.RunnerRole) ([]*traces.CommitteeDutyTrace, error)
	GetCommitteeDutyLink(slot phase0.Slot, index phase0.ValidatorIndex) (spectypes.CommitteeID, error)
	GetCommitteeDutyLinks(slot phase0.Slot) ([]*traces.CommitteeDutyLink, error)
	GetValidatorDuty(slot phase0.Slot, role spectypes.BeaconRole, index phase0.ValidatorIndex) (*traces.ValidatorDutyTrace, error)
	GetValidatorDuties(role spectypes.BeaconRole, slot phase0.Slot) ([]*traces.ValidatorDutyTrace, error)

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

func (c *Collector) GetValidatorDuties(role spectypes.BeaconRole, slot phase0.Slot) ([]*traces.ValidatorDutyTrace, error) {
	duties := []*traces.ValidatorDutyTrace{}
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

func (c *Collector) getValidatorDutiesFromDisk(role spectypes.BeaconRole, slot phase0.Slot) ([]*traces.ValidatorDutyTrace, error) {
	var errs *multierror.Error

	storeDuties, err := c.store.GetValidatorDuties(role, slot)
	errs = multierror.Append(errs, err)

	duties := make([]*traces.ValidatorDutyTrace, 0, len(storeDuties))
	for _, duty := range storeDuties {
		duties = append(duties, duty.DeepCopy())
	}
	return duties, errs.ErrorOrNil()
}

func (c *Collector) GetValidatorDuty(role spectypes.BeaconRole, slot phase0.Slot, index phase0.ValidatorIndex) (*traces.ValidatorDutyTrace, error) {
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

func (c *Collector) getValidatorDutyFromDiskIndex(role spectypes.BeaconRole, slot phase0.Slot, index phase0.ValidatorIndex) (*traces.ValidatorDutyTrace, error) {
	trace, err := c.store.GetValidatorDuty(slot, role, index)
	if err != nil {
		return nil, fmt.Errorf("get validator duty from disk (role=%s slot=%d index=%d): %w", role, slot, index, err)
	}

	return trace, nil
}

func (c *Collector) GetCommitteeDuties(wantSlot phase0.Slot, roles ...spectypes.RunnerRole) ([]*traces.CommitteeDutyTrace, error) {
	var duties []*traces.CommitteeDutyTrace
	var errs *multierror.Error

	c.committeeTraces.Range(func(key committeeTraceKey, committeeSlots *hashmap.Map[phase0.Slot, *committeeDutyTrace]) bool {
		if len(roles) > 0 && !slices.Contains(roles, key.role) {
			return true
		}
		dt, found := committeeSlots.Get(wantSlot)
		if found {
			duties = append(duties, dt.safeDeepCopy())
		}
		return true // keep iterating
	})

	diskDuties, err := c.store.GetCommitteeDuties(wantSlot, roles...)
	duties = append(duties, diskDuties...)
	errs = multierror.Append(errs, err)
	return duties, errs.ErrorOrNil()
}

func (c *Collector) GetCommitteeDuty(slot phase0.Slot, committeeID spectypes.CommitteeID, role spectypes.RunnerRole) (*traces.CommitteeDutyTrace, error) {
	key := committeeTraceKey{id: committeeID, role: role}
	if committeeSlots, found := c.committeeTraces.Get(key); found {
		if trace, found := committeeSlots.Get(slot); found {
			return trace.safeDeepCopy(), nil
		}
	}

	trace, err := c.getCommitteeDutyFromDisk(slot, role, committeeID)
	if err != nil {
		return nil, err
	}

	return trace, nil
}

// hasSignersForRoles checks if the duty has signers for the given beacon role(s).
// Committee duties are keyed by runner role, but we still use the per-role signer
// buckets to distinguish between attester vs aggregator and sync committee vs SCC.
// Callers should handle the "no role filter" case before invoking this helper.
func hasSignersForRoles(duty *traces.CommitteeDutyTrace, roles ...spectypes.BeaconRole) bool {
	// Signer buckets are role-specific; use them to ensure the duty matches the requested beacon roles.
	for _, role := range roles {
		bucket, ok := ssvtypes.CommitteeSignerBucketForBeaconRole(role)
		if !ok {
			continue
		}
		switch bucket {
		case ssvtypes.CommitteeSignerBucketAttester:
			if len(duty.Attester) == 0 {
				return false
			}
		case ssvtypes.CommitteeSignerBucketSyncCommittee:
			if len(duty.SyncCommittee) == 0 {
				return false
			}
		case ssvtypes.CommitteeSignerBucketUnknown:
			// Not a committee signer bucket.
		}
	}
	return true
}

func (c *Collector) getCommitteeDutyFromDisk(slot phase0.Slot, role spectypes.RunnerRole, committeeID spectypes.CommitteeID) (*traces.CommitteeDutyTrace, error) {
	ctx := fmt.Sprintf("slot=%d role=%d committeeID=%x", slot, role, committeeID)
	trace, err := c.store.GetCommitteeDuty(slot, role, committeeID)
	if err != nil {
		return nil, fmt.Errorf("get committee duty from disk (%s): %w", ctx, err)
	}

	return trace, nil
}

func committeeRunnerRolesForBeaconRoles(roles ...spectypes.BeaconRole) []spectypes.RunnerRole {
	if len(roles) == 0 {
		return nil
	}
	var wantCommittee bool
	var wantAggregator bool
	for _, role := range roles {
		runnerRole, ok := ssvtypes.CommitteeRunnerRoleForBeaconRole(role)
		if !ok {
			continue
		}
		switch runnerRole {
		case spectypes.RoleCommittee:
			wantCommittee = true
		case spectypes.RoleAggregatorCommittee:
			wantAggregator = true
		default:
			// Not a committee runner role.
		}
	}
	out := make([]spectypes.RunnerRole, 0, 2)
	if wantCommittee {
		out = append(out, spectypes.RoleCommittee)
	}
	if wantAggregator {
		out = append(out, spectypes.RoleAggregatorCommittee)
	}
	return out
}

func (c *Collector) GetAllCommitteeDecideds(slot phase0.Slot, roles ...spectypes.BeaconRole) ([]ParticipantsRangeIndexEntry, error) {
	var errs *multierror.Error

	runnerRoles := committeeRunnerRolesForBeaconRoles(roles...)
	if len(roles) > 0 && len(runnerRoles) == 0 {
		// A role filter was requested but none of the roles map to a committee
		// runner role, so nothing can match. Return empty rather than querying
		// with no filter (which would return every committee duty).
		return nil, nil
	}
	duties, err := c.GetCommitteeDuties(slot, runnerRoles...)
	errs = multierror.Append(errs, err)
	if len(duties) == 0 {
		return nil, errs.ErrorOrNil()
	}

	if len(roles) > 0 {
		filtered := make([]*traces.CommitteeDutyTrace, 0, len(duties))
		for _, duty := range duties {
			if hasSignersForRoles(duty, roles...) {
				filtered = append(filtered, duty)
			}
		}
		duties = filtered
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

func (c *Collector) GetCommitteeDutyLinks(slot phase0.Slot) ([]*traces.CommitteeDutyLink, error) {
	out := make([]*traces.CommitteeDutyLink, 0)
	var errs *multierror.Error

	c.validatorIndexToCommitteeLinks.Range(func(vi phase0.ValidatorIndex, m *hashmap.Map[phase0.Slot, spectypes.CommitteeID]) bool {
		cid, found := m.Get(slot)
		if found {
			out = append(out, &traces.CommitteeDutyLink{
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

	runnerRoles := committeeRunnerRolesForBeaconRoles(roles...)
	if len(roles) > 0 && len(runnerRoles) == 0 {
		// A role filter was requested but none of the roles map to a committee
		// runner role, so nothing can match.
		return nil, fmt.Errorf("get committee duty: %w", ErrNotFound)
	}
	if len(runnerRoles) == 0 {
		runnerRoles = []spectypes.RunnerRole{spectypes.RoleCommittee, spectypes.RoleAggregatorCommittee}
	}

	var duty *traces.CommitteeDutyTrace
	for _, role := range runnerRoles {
		d, dutyErr := c.GetCommitteeDuty(slot, committeeID, role)
		if dutyErr != nil {
			if errors.Is(dutyErr, ErrNotFound) {
				continue
			}
			return nil, fmt.Errorf("get committee duty: %w", dutyErr)
		}
		if len(roles) == 0 || hasSignersForRoles(d, roles...) {
			duty = d
			break
		}
	}
	if duty == nil {
		return nil, fmt.Errorf("get committee duty: %w", ErrNotFound)
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
