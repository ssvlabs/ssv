package validator

import (
	"encoding/hex"
	"fmt"
	"slices"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	model "github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/observability/log/fields"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
	"github.com/ssvlabs/ssv/utils/hashmap"
)

var ErrNotFound = errors.New("not found")

// ValidatorDutyTrace is a wrapper around the model.ValidatorDutyTrace that adds a CommitteeID and pubkey field
// to avoid extra lookups
type ValidatorDutyTrace struct {
	model.ValidatorDutyTrace
	CommitteeID spectypes.CommitteeID
	pubkey      spectypes.ValidatorPK
}

// implemented by DutyStoreMetrics
type DutyTraceStore interface {
	SaveCommitteeDutyLink(slot phase0.Slot, index phase0.ValidatorIndex, id spectypes.CommitteeID) error
	SaveCommitteeDutyLinks(slot phase0.Slot, linkMap map[phase0.ValidatorIndex]spectypes.CommitteeID) error
	SaveCommitteeDuty(duty *model.CommitteeDutyTrace) error
	SaveCommitteeDuties(slot phase0.Slot, duties []*model.CommitteeDutyTrace) error
	SaveValidatorDuty(duty *model.ValidatorDutyTrace) error
	SaveValidatorDuties(duties []*model.ValidatorDutyTrace) error
	GetCommitteeDuty(slot phase0.Slot, committeeID spectypes.CommitteeID) (*model.CommitteeDutyTrace, error)
	GetCommitteeDuties(slot phase0.Slot) ([]*model.CommitteeDutyTrace, error)
	GetCommitteeDutyLink(slot phase0.Slot, index phase0.ValidatorIndex) (spectypes.CommitteeID, error)
	GetCommitteeDutyLinks(slot phase0.Slot) ([]*model.CommitteeDutyLink, error)
	GetValidatorDuty(slot phase0.Slot, role spectypes.BeaconRole, index phase0.ValidatorIndex) (*model.ValidatorDutyTrace, error)
	GetValidatorDuties(role spectypes.BeaconRole, slot phase0.Slot) ([]*model.ValidatorDutyTrace, error)
}

func (c *Collector) GetCommitteeID(slot phase0.Slot, pubkey spectypes.ValidatorPK) (spectypes.CommitteeID, phase0.ValidatorIndex, error) {
	index, found := c.validators.ValidatorIndex(pubkey)
	if !found {
		return spectypes.CommitteeID{}, 0, fmt.Errorf("validator not found")
	}

	committeeID, err := c.getCommitteeIDBySlotAndIndex(slot, index)
	if err != nil {
		return spectypes.CommitteeID{}, 0, fmt.Errorf("get committee ID: %w", err)
	}

	return committeeID, index, nil
}

func (c *Collector) GetValidatorDuty(role spectypes.BeaconRole, slot phase0.Slot, pubkey spectypes.ValidatorPK) (*ValidatorDutyTrace, error) {
	// lookup in cache
	validatorSlots, found := c.validatorTraces.Get(pubkey)
	if !found {
		return c.getValidatorDutiesFromDisk(role, slot, pubkey)
	}

	traces, found := validatorSlots.Get(slot)
	if found {
		traces.Lock()
		defer traces.Unlock()

		// find the trace for the role
		for _, trace := range traces.roles {
			if trace.Role == role {
				return &ValidatorDutyTrace{
					ValidatorDutyTrace: *deepCopyValidatorDutyTrace(trace),
					pubkey:             pubkey,
				}, nil
			}
		}
	}

	// go to disk for the older ones
	return c.getValidatorDutiesFromDisk(role, slot, pubkey)
}

func (c *Collector) getValidatorDutiesFromDisk(role spectypes.BeaconRole, slot phase0.Slot, pubkey spectypes.ValidatorPK) (*ValidatorDutyTrace, error) {
	vIndex, found := c.validators.ValidatorIndex(pubkey)
	if !found {
		return nil, fmt.Errorf("validator not found by pubkey: %x", pubkey)
	}

	trace, err := c.store.GetValidatorDuty(slot, role, vIndex)
	if err != nil {
		return nil, fmt.Errorf("get validator duty from disk: %w", err)
	}

	return &ValidatorDutyTrace{
		ValidatorDutyTrace: *trace,
		pubkey:             pubkey,
	}, nil
}

func (c *Collector) GetCommitteeDuties(wantSlot phase0.Slot, roles ...spectypes.BeaconRole) (duties []*model.CommitteeDutyTrace, err error) {
	c.committeeTraces.Range(func(committeeID spectypes.CommitteeID, committeeSlots *hashmap.Map[phase0.Slot, *committeeDutyTrace]) bool {
		dt, found := committeeSlots.Get(wantSlot)
		if !found {
			return true
		}

		duties = append(duties, dt.trace())

		return true
	})

	diskDuties, err := c.store.GetCommitteeDuties(wantSlot)
	if err != nil {
		return nil, fmt.Errorf("get committee duties from disk: %w", err)
	}

	duties = append(duties, diskDuties...)

	var filteredDuties []*model.CommitteeDutyTrace
	for _, duty := range duties {
		if hasSignersForRoles(duty, roles...) {
			filteredDuties = append(filteredDuties, duty)
		}
	}

	return filteredDuties, nil
}

func (c *Collector) GetCommitteeDuty(slot phase0.Slot, committeeID spectypes.CommitteeID, roles ...spectypes.BeaconRole) (*model.CommitteeDutyTrace, error) {
	committeeSlots, found := c.committeeTraces.Get(committeeID)
	if !found {
		trace, err := c.getCommitteeDutyFromDisk(slot, committeeID)
		if err != nil {
			return nil, fmt.Errorf("get committee duty from disk: %w", err)
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
			return nil, fmt.Errorf("get committee duty from disk: %w", err)
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

// hasSignersForRole checks if the duty has signers for the given role
// since we don't store a boolean flag to separate duties by their role in the db
// we rely on the fact that during collection we separate the signers in their
// corresponding fields (Attester and SyncCommittee) based on the role
func hasSignersForRoles(duty *model.CommitteeDutyTrace, roles ...spectypes.BeaconRole) bool {
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

func (c *Collector) getCommitteeDutyFromDisk(slot phase0.Slot, committeeID spectypes.CommitteeID) (*model.CommitteeDutyTrace, error) {
	trace, err := c.store.GetCommitteeDuty(slot, committeeID)
	if err != nil {
		return nil, fmt.Errorf("get committee duty from disk: %w", err)
	}

	return trace, nil
}

func (c *Collector) GetAllCommitteeDecideds(slot phase0.Slot, roles ...spectypes.BeaconRole) (out []qbftstorage.ParticipantsRangeEntry, err error) {
	duties, err := c.GetCommitteeDuties(slot, roles...)
	if err != nil {
		return nil, fmt.Errorf("get committee duties: %w", err)
	}

	links, err := c.GetCommitteeDutyLinks(slot)
	if err != nil {
		return nil, fmt.Errorf("get committee duty links: %w", err)
	}

	mapping := make(map[spectypes.CommitteeID]spectypes.ValidatorPK)
	for _, link := range links {
		share, found := c.validators.ValidatorByIndex(link.ValidatorIndex)
		if !found {
			c.logger.Error("validator not found", fields.ValidatorIndex(link.ValidatorIndex))
			continue
		}
		mapping[link.CommitteeID] = share.ValidatorPubKey
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

		out = append(out, qbftstorage.ParticipantsRangeEntry{
			Slot:    slot,
			PubKey:  mapping[duty.CommitteeID],
			Signers: signers,
		})
	}

	return out, nil
}

func (c *Collector) GetCommitteeDutyLinks(slot phase0.Slot) (out []*model.CommitteeDutyLink, err error) {
	c.validatorIndexToCommitteeLinks.Range(func(vi phase0.ValidatorIndex, m *hashmap.Map[phase0.Slot, spectypes.CommitteeID]) bool {
		cid, found := m.Get(slot)
		if found {
			out = append(out, &model.CommitteeDutyLink{
				ValidatorIndex: vi,
				CommitteeID:    cid,
			})
		}
		return true
	})

	links, err := c.store.GetCommitteeDutyLinks(slot)
	if err != nil {
		return nil, fmt.Errorf("get committee duty links: %w", err)
	}
	out = append(out, links...)
	return out, nil
}

func (c *Collector) GetCommitteeDecideds(slot phase0.Slot, pubkey spectypes.ValidatorPK, roles ...spectypes.BeaconRole) (out []qbftstorage.ParticipantsRangeEntry, err error) {
	index, found := c.validators.ValidatorIndex(pubkey)
	if !found {
		return nil, fmt.Errorf("validator not found: %s", hex.EncodeToString(pubkey[:]))
	}

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

	out = append(out, qbftstorage.ParticipantsRangeEntry{
		Slot:    slot,
		PubKey:  pubkey,
		Signers: signers,
	})

	return out, nil
}

func (c *Collector) GetValidatorDecideds(role spectypes.BeaconRole, slot phase0.Slot, pubkeys []spectypes.ValidatorPK) (out []qbftstorage.ParticipantsRangeEntry, err error) {
	for _, pubkey := range pubkeys {
		duty, err := c.GetValidatorDuty(role, slot, pubkey)
		if err != nil {
			return nil, fmt.Errorf("get validator duty for decideds: %w", err)
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

		out = append(out, qbftstorage.ParticipantsRangeEntry{
			Slot:    slot,
			PubKey:  duty.pubkey,
			Signers: signers,
		})
	}

	return
}

func (c *Collector) GetAllValidatorDecideds(role spectypes.BeaconRole, slot phase0.Slot) (out []qbftstorage.ParticipantsRangeEntry, err error) {
	duties, err := c.store.GetValidatorDuties(role, slot)
	if err != nil {
		return nil, fmt.Errorf("get all validator duties: %w", err)
	}

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

		share, found := c.validators.ValidatorByIndex(duty.Validator)
		if !found {
			c.logger.Error("validator not found", fields.ValidatorIndex(duty.Validator))
			continue
		}

		out = append(out, qbftstorage.ParticipantsRangeEntry{
			Slot:    slot,
			PubKey:  share.ValidatorPubKey,
			Signers: signers,
		})
	}

	return out, nil
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
	link, err := c.store.GetCommitteeDutyLink(slot, index)
	if err != nil {
		return spectypes.CommitteeID{}, fmt.Errorf("get from disk: %w", err)
	}

	return link, nil
}

func deepCopyCommitteeDutyTrace(trace *model.CommitteeDutyTrace) *model.CommitteeDutyTrace {
	return &model.CommitteeDutyTrace{
		ConsensusTrace: model.ConsensusTrace{
			Rounds:   deepCopyRounds(trace.Rounds),
			Decideds: deepCopyDecideds(trace.Decideds),
		},
		Slot:          trace.Slot,
		CommitteeID:   trace.CommitteeID,
		OperatorIDs:   deepCopyOperatorIDs(trace.OperatorIDs),
		SyncCommittee: deepCopySigners(trace.SyncCommittee),
		Attester:      deepCopySigners(trace.Attester),
		ProposalData:  deepCopyProposalData(trace.ProposalData),
	}
}

func deepCopyDecideds(decideds []*model.DecidedTrace) []*model.DecidedTrace {
	cp := make([]*model.DecidedTrace, len(decideds))
	for i, d := range decideds {
		cp[i] = deepCopyDecided(d)
	}
	return cp
}

func deepCopyDecided(trace *model.DecidedTrace) *model.DecidedTrace {
	return &model.DecidedTrace{
		Round:        trace.Round,
		BeaconRoot:   trace.BeaconRoot,
		Signers:      deepCopyOperatorIDs(trace.Signers),
		ReceivedTime: trace.ReceivedTime,
	}
}

func deepCopyRounds(rounds []*model.RoundTrace) []*model.RoundTrace {
	cp := make([]*model.RoundTrace, len(rounds))
	for i, r := range rounds {
		cp[i] = deepCopyRound(r)
	}
	return cp
}

func deepCopyRound(round *model.RoundTrace) *model.RoundTrace {
	return &model.RoundTrace{
		Proposer:      round.Proposer,
		Prepares:      deepCopyPrepares(round.Prepares),
		ProposalTrace: deepCopyProposalTrace(round.ProposalTrace),
		Commits:       deepCopyCommits(round.Commits),
		RoundChanges:  deepCopyRoundChanges(round.RoundChanges),
	}
}

func deepCopyProposalTrace(trace *model.ProposalTrace) *model.ProposalTrace {
	if trace == nil {
		return nil
	}

	return &model.ProposalTrace{
		QBFTTrace: model.QBFTTrace{
			Round:        trace.Round,
			BeaconRoot:   trace.BeaconRoot,
			Signer:       trace.Signer,
			ReceivedTime: trace.ReceivedTime,
		},
		RoundChanges:    deepCopyRoundChanges(trace.RoundChanges),
		PrepareMessages: deepCopyPrepares(trace.PrepareMessages),
	}
}

func deepCopyCommits(commits []*model.QBFTTrace) []*model.QBFTTrace {
	cp := make([]*model.QBFTTrace, len(commits))
	for i, c := range commits {
		cp[i] = deepCopyQBFTTrace(c)
	}
	return cp
}

func deepCopyRoundChanges(roundChanges []*model.RoundChangeTrace) []*model.RoundChangeTrace {
	cp := make([]*model.RoundChangeTrace, len(roundChanges))
	for i, r := range roundChanges {
		cp[i] = deepCopyRoundChange(r)
	}
	return cp
}

func deepCopyRoundChange(trace *model.RoundChangeTrace) *model.RoundChangeTrace {
	return &model.RoundChangeTrace{
		QBFTTrace: model.QBFTTrace{
			Round:        trace.Round,
			BeaconRoot:   trace.BeaconRoot,
			Signer:       trace.Signer,
			ReceivedTime: trace.ReceivedTime,
		},
		PreparedRound:   trace.PreparedRound,
		PrepareMessages: deepCopyPrepares(trace.PrepareMessages),
	}
}

func deepCopyPrepares(prepares []*model.QBFTTrace) []*model.QBFTTrace {
	cp := make([]*model.QBFTTrace, len(prepares))
	for i, p := range prepares {
		cp[i] = deepCopyQBFTTrace(p)
	}
	return cp
}

func deepCopyQBFTTrace(trace *model.QBFTTrace) *model.QBFTTrace {
	return &model.QBFTTrace{
		Round:        trace.Round,
		Signer:       trace.Signer,
		ReceivedTime: trace.ReceivedTime,
		BeaconRoot:   trace.BeaconRoot,
	}
}

func deepCopySigners(committee []*model.SignerData) []*model.SignerData {
	cp := make([]*model.SignerData, len(committee))
	for i, c := range committee {
		cp[i] = deepCopySignerData(c)
	}
	return cp
}

func deepCopySignerData(data *model.SignerData) *model.SignerData {
	return &model.SignerData{
		Signer:       data.Signer,
		ValidatorIdx: data.ValidatorIdx,
		ReceivedTime: data.ReceivedTime,
	}
}

func deepCopyOperatorIDs(ids []spectypes.OperatorID) []spectypes.OperatorID {
	cp := make([]spectypes.OperatorID, len(ids))
	copy(cp, ids)
	return cp
}

func deepCopyValidatorDutyTrace(trace *model.ValidatorDutyTrace) *model.ValidatorDutyTrace {
	return &model.ValidatorDutyTrace{
		ConsensusTrace: model.ConsensusTrace{
			Rounds:   trace.Rounds,
			Decideds: trace.Decideds,
		},
		Slot:         trace.Slot,
		Role:         trace.Role,
		Validator:    trace.Validator,
		Pre:          deepCopyPartialSigs(trace.Pre),
		Post:         deepCopyPartialSigs(trace.Post),
		ProposalData: deepCopyProposalData(trace.ProposalData),
	}
}

func deepCopyProposalData(data []byte) []byte {
	cp := make([]byte, len(data))
	copy(cp, data)
	return cp
}

func deepCopyPartialSigs(partialSigs []*model.PartialSigTrace) []*model.PartialSigTrace {
	cp := make([]*model.PartialSigTrace, len(partialSigs))
	for i, p := range partialSigs {
		cp[i] = deepCopyPartialSig(p)
	}
	return cp
}

func deepCopyPartialSig(trace *model.PartialSigTrace) *model.PartialSigTrace {
	return &model.PartialSigTrace{
		Type:         trace.Type,
		BeaconRoot:   trace.BeaconRoot,
		Signer:       trace.Signer,
		ReceivedTime: trace.ReceivedTime,
	}
}
