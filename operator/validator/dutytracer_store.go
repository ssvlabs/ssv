package validator

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	model "github.com/ssvlabs/ssv/exporter/v2"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/operator/slotticker"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"go.uber.org/zap"
)

// adapter is a wrapper over tracer to provide getters only
type adapter struct {
	tracer *InMemTracer
}

func (a *adapter) GetValidatorDuties(role spectypes.BeaconRole, slot phase0.Slot, pubkeys []spectypes.ValidatorPK) ([]*model.ValidatorDutyTrace, error) {
	traces := make([]*model.ValidatorDutyTrace, 0, len(pubkeys))
	for _, pubkey := range pubkeys {
		trace, err := a.tracer.getValidatorDuty(role, slot, pubkey)
		if err != nil {
			return nil, err
		}
		traces = append(traces, trace)
	}

	return traces, nil
}

func (a *adapter) GetCommitteeDuty(slot phase0.Slot, committeeID spectypes.CommitteeID) (*model.CommitteeDutyTrace, error) {
	return a.tracer.getCommitteeDuty(slot, committeeID)
}

func (a *adapter) committeeDecideds(slot phase0.Slot, pubkeys []spectypes.ValidatorPK) (out []qbftstorage.ParticipantsRangeEntry, err error) {
	for _, pubkey := range pubkeys {
		index, found := a.tracer.validators.ValidatorIndex(pubkey)
		if !found {
			a.tracer.logger.Error("validator not found", fields.Validator(pubkey[:]))
			continue
		}

		committeeID, err := a.tracer.getCommitteeIDBySlotAndIndex(slot, index)
		if err != nil {
			return nil, err
		}

		duty, err := a.tracer.getCommitteeDuty(slot, committeeID)
		if err != nil {
			return nil, err
		}

		var signers []spectypes.OperatorID
		// TODO(matheus) is this correct?
		for _, d := range duty.Decideds {
			signers = append(signers, d.Signers...)
		}

		out = append(out, qbftstorage.ParticipantsRangeEntry{
			Slot:    slot,
			PubKey:  pubkey,
			Signers: signers,
		})
	}

	return
}

func (a *adapter) GetDecideds(role spectypes.BeaconRole, slot phase0.Slot, pubkeys []spectypes.ValidatorPK) (out []qbftstorage.ParticipantsRangeEntry) {
	switch role {
	case spectypes.BNRoleSyncCommittee:
		fallthrough
	case spectypes.BNRoleAttester:
		decideds, err := a.committeeDecideds(slot, pubkeys)
		if err != nil {
			a.tracer.logger.Error("failed to get committee decideds", zap.Error(err))
			return nil
		}
		out = append(out, decideds...)
		return
	default: // validator
		for _, pubkey := range pubkeys {
			duty, err := a.tracer.getValidatorDuty(role, slot, pubkey)
			if err != nil {
				a.tracer.logger.Error("failed to get validator decideds", zap.Error(err), fields.Validator(pubkey[:]))
				continue
			}

			var signers []spectypes.OperatorID
			// TODO(matheus) is this correct?
			for _, d := range duty.Decideds {
				signers = append(signers, d.Signers...)
			}

			out = append(out, qbftstorage.ParticipantsRangeEntry{
				Slot:    slot,
				PubKey:  pubkey,
				Signers: signers,
			})
		}
	}

	return
}

// noOpTracer is a placeholder tracer, not meant to be called.
type noOpTracer struct{}

func NoOp() *noOpTracer {
	return new(noOpTracer)
}

func (n *noOpTracer) Trace(*queue.SSVMessage) {}
func (n *noOpTracer) Store() DutyTraceStore {
	return new(noOpStore)
}
func (n *noOpTracer) StartEvictionJob(ctx context.Context, ticker slotticker.Provider) {
	panic("not implemented")
}

type noOpStore struct{}

func (n *noOpStore) GetValidatorDuties(spectypes.BeaconRole, phase0.Slot, []spectypes.ValidatorPK) ([]*model.ValidatorDutyTrace, error) {
	panic("not implemented")
}
func (n *noOpStore) GetCommitteeDutiesByOperator([]spectypes.OperatorID, phase0.Slot) ([]*model.CommitteeDutyTrace, error) {
	panic("not implemented")
}
func (n *noOpStore) GetCommitteeDuty(phase0.Slot, spectypes.CommitteeID) (*model.CommitteeDutyTrace, error) {
	panic("not implemented")
}
func (n *noOpStore) GetAllValidatorDuties(spectypes.BeaconRole, phase0.Slot) ([]*model.ValidatorDutyTrace, error) {
	panic("not implemented")
}
func (n *noOpStore) GetDecideds(role spectypes.BeaconRole, slot phase0.Slot, pubKey []spectypes.ValidatorPK) []qbftstorage.ParticipantsRangeEntry {
	panic("not implemented")
}
