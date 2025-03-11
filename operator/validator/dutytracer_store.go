package validator

import (
	"context"
	"encoding/hex"
	"fmt"
	"slices"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	model "github.com/ssvlabs/ssv/exporter/v2"
	"github.com/ssvlabs/ssv/logging/fields"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
)

type ValidatorDutyTrace struct {
	model.ValidatorDutyTrace
	pubkey spectypes.ValidatorPK
}

func (a *InMemTracer) GetValidatorDuties(role spectypes.BeaconRole, slot phase0.Slot, pubkey spectypes.ValidatorPK) (*ValidatorDutyTrace, error) {
	// lookup in cache
	validatorSlots, found := a.validatorTraces.Load(pubkey)
	if !found {
		// should only happen if we request a validator duty right after startup
		return nil, errors.New("validator not found")
	}

	traces, found := validatorSlots.Load(slot)
	if found {
		traces.Lock()
		defer traces.Unlock()

		// find the trace for the role
		for _, trace := range traces.Roles {
			if trace.Role == role {
				return &ValidatorDutyTrace{
					ValidatorDutyTrace: *deepCopyValidatorDutyTrace(trace),
					pubkey:             pubkey,
				}, nil
			}
		}
	}

	// go to disk for the older ones
	vIndex, found := a.validators.ValidatorIndex(pubkey)
	if !found {
		return nil, fmt.Errorf("validator not found by pubkey: %x", pubkey)
	}

	start := time.Now()

	trace, err := a.store.GetValidatorDuty(slot, role, vIndex)
	if err != nil {
		return nil, fmt.Errorf("get validator duty from disk: %w", err)
	}

	duration := time.Since(start)
	tracerDBDurationHistogram.Record(
		context.Background(),
		duration.Seconds(),
		metric.WithAttributes(
			semconv.DBCollectionName("validator"),
			semconv.DBOperationName("get"),
		),
	)

	return &ValidatorDutyTrace{
		ValidatorDutyTrace: *trace,
		pubkey:             pubkey,
	}, nil
}

func (imt *InMemTracer) GetCommitteeDuty(slot phase0.Slot, committeeID spectypes.CommitteeID) (*model.CommitteeDutyTrace, error) {
	committeeSlots, found := imt.committeeTraces.Load(committeeID)
	if !found {
		// after "warm-up" there is always going to be a committee key in the map
		return nil, errors.New("committee not found")
	}

	trace, found := committeeSlots.Load(slot)
	if !found {
		start := time.Now()
		trace, err := imt.store.GetCommitteeDuty(slot, committeeID)
		if err != nil {
			return nil, fmt.Errorf("get committee duty from disk: %w", err)
		}

		duration := time.Since(start)
		tracerDBDurationHistogram.Record(
			context.Background(),
			duration.Seconds(),
			metric.WithAttributes(
				semconv.DBCollectionName("committee"),
				semconv.DBOperationName("get"),
			),
		)

		if trace != nil {
			return trace, nil
		}

		return nil, errors.New("slot not found")
	}

	trace.Lock()
	defer trace.Unlock()

	return deepCopyCommitteeDutyTrace(&trace.CommitteeDutyTrace), nil
}

func (imt *InMemTracer) GetCommitteeDecideds(slot phase0.Slot, pubkey spectypes.ValidatorPK) (out []qbftstorage.ParticipantsRangeEntry, err error) {
	index, found := imt.validators.ValidatorIndex(pubkey)
	if !found {
		return nil, fmt.Errorf("validator not found: %s", hex.EncodeToString(pubkey[:]))
	}

	committeeID, err := imt.getCommitteeIDBySlotAndIndex(slot, index)
	if err != nil {
		return nil, fmt.Errorf("get committee ID by slot(%d) and index(%d): %w", slot, index, err)
	}

	duty, err := imt.GetCommitteeDuty(slot, committeeID)
	if err != nil {
		return nil, fmt.Errorf("get committee duty: %w", err)
	}

	var signers []spectypes.OperatorID
	// TODO(matheus) is this correct?
	for _, d := range duty.Decideds {
		signers = append(signers, d.Signers...)
	}

	slices.Sort(signers)

	out = append(out, qbftstorage.ParticipantsRangeEntry{
		Slot:    slot,
		PubKey:  pubkey,
		Signers: slices.Compact(signers),
	})

	return out, nil
}

func (imt *InMemTracer) GetValidatorDecideds(role spectypes.BeaconRole, slot phase0.Slot, pubkeys []spectypes.ValidatorPK) (out []qbftstorage.ParticipantsRangeEntry, err error) {
	for _, pubkey := range pubkeys {
		duty, err := imt.GetValidatorDuties(role, slot, pubkey)
		if err != nil {
			return nil, fmt.Errorf("get validator duties for decideds: %w", err)
		}

		var signers []spectypes.OperatorID
		// TODO(matheus) is this correct? if decideds empty, return err?
		for _, d := range duty.Decideds {
			signers = append(signers, d.Signers...)
		}

		slices.Sort(signers)

		out = append(out, qbftstorage.ParticipantsRangeEntry{
			Slot:    slot,
			PubKey:  duty.pubkey,
			Signers: slices.Compact(signers),
		})
	}

	return
}

func (imt *InMemTracer) getCommitteeIDBySlotAndIndex(slot phase0.Slot, index phase0.ValidatorIndex) (spectypes.CommitteeID, error) {
	slotToCommittee, found := imt.validatorIndexToCommitteeLinks.Load(index)
	if !found {
		return spectypes.CommitteeID{}, fmt.Errorf("committee not found by index: %d", index)
	}

	committeeID, found := slotToCommittee.Load(slot)
	if !found {
		start := time.Now()

		imt.logger.Info("-committee decideds- loading from disk", fields.Slot(slot), fields.ValidatorIndex(index))

		link, err := imt.store.GetCommitteeDutyLink(slot, index)
		if err != nil {
			return spectypes.CommitteeID{}, fmt.Errorf("get committee duty link from disk: %w", err)
		}

		duration := time.Since(start)
		tracerDBDurationHistogram.Record(
			context.Background(),
			duration.Seconds(),
			metric.WithAttributes(
				semconv.DBCollectionName("committee"),
				semconv.DBOperationName("get"),
			),
		)

		return link, nil
	}

	return committeeID, nil
}

func decodeSig(in []byte) (*bls.Sign, error) {
	sign := new(bls.Sign)
	err := sign.Deserialize(in)
	return sign, err
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
	}
}

func deepCopyDecideds(decideds []*model.DecidedTrace) []*model.DecidedTrace {
	copy := make([]*model.DecidedTrace, len(decideds))
	for i, d := range decideds {
		copy[i] = deepCopyDecided(d)
	}
	return copy
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
	copy := make([]*model.RoundTrace, len(rounds))
	for i, r := range rounds {
		copy[i] = deepCopyRound(r)
	}
	return copy
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
	copy := make([]*model.QBFTTrace, len(commits))
	for i, c := range commits {
		copy[i] = deepCopyQBFTTrace(c)
	}
	return copy
}

func deepCopyRoundChanges(roundChanges []*model.RoundChangeTrace) []*model.RoundChangeTrace {
	copy := make([]*model.RoundChangeTrace, len(roundChanges))
	for i, r := range roundChanges {
		copy[i] = deepCopyRoundChange(r)
	}
	return copy
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
	copy := make([]*model.QBFTTrace, len(prepares))
	for i, p := range prepares {
		copy[i] = deepCopyQBFTTrace(p)
	}
	return copy
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
	copy := make([]*model.SignerData, len(committee))
	for i, c := range committee {
		copy[i] = deepCopySignerData(c)
	}
	return copy
}

func deepCopySignerData(data *model.SignerData) *model.SignerData {
	return &model.SignerData{
		Signers:      deepCopyOperatorIDs(data.Signers),
		ReceivedTime: data.ReceivedTime,
	}
}

func deepCopyOperatorIDs(ids []spectypes.OperatorID) []spectypes.OperatorID {
	copy := make([]spectypes.OperatorID, 0, len(ids))
	copy = append(copy, ids...)
	return copy
}

func deepCopyValidatorDutyTrace(trace *model.ValidatorDutyTrace) *model.ValidatorDutyTrace {
	return &model.ValidatorDutyTrace{
		ConsensusTrace: model.ConsensusTrace{
			Rounds:   trace.Rounds,
			Decideds: trace.Decideds,
		},
		Slot:      trace.Slot,
		Role:      trace.Role,
		Validator: trace.Validator,
		Pre:       deepCopyPartialSigs(trace.Pre),
		Post:      deepCopyPartialSigs(trace.Post),
	}
}

func deepCopyPartialSigs(partialSigs []*model.PartialSigTrace) []*model.PartialSigTrace {
	copy := make([]*model.PartialSigTrace, len(partialSigs))
	for i, p := range partialSigs {
		copy[i] = deepCopyPartialSig(p)
	}
	return copy
}

func deepCopyPartialSig(trace *model.PartialSigTrace) *model.PartialSigTrace {
	return &model.PartialSigTrace{
		Type:         trace.Type,
		BeaconRoot:   trace.BeaconRoot,
		Signer:       trace.Signer,
		ReceivedTime: trace.ReceivedTime,
	}
}
