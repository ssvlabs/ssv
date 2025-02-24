package validator

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	model "github.com/ssvlabs/ssv/exporter/v2"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"go.uber.org/zap"
)

type adapter struct {
	tracer *InMemTracer
	logger *zap.Logger
}

func (a *adapter) GetValidatorDuty(role spectypes.BeaconRole, slot phase0.Slot, vIndex phase0.ValidatorIndex) (*model.ValidatorDutyTrace, error) {
	trace, err := a.tracer.getValidatorDuty(role, slot, vIndex)
	if err != nil {
		return nil, err
	}

	return &model.ValidatorDutyTrace{
		Slot: slot,
		ConsensusTrace: model.ConsensusTrace{
			Rounds:   trace.Rounds,
			Decideds: trace.Decideds,
		},
		Post: trace.Post,
	}, nil
}

func (a *adapter) GetCommitteeDutiesByOperator(indexes []spectypes.OperatorID, slot phase0.Slot) ([]*model.CommitteeDutyTrace, error) {
	panic("not implemented")
}

func (a *adapter) GetCommitteeDuty(slot phase0.Slot, committeeID spectypes.CommitteeID) (*model.CommitteeDutyTrace, error) {
	trace, err := a.tracer.getCommitteeDuty(slot, committeeID)
	if err != nil {
		return nil, err
	}

	return &model.CommitteeDutyTrace{
		Slot:        slot,
		CommitteeID: committeeID,
		ConsensusTrace: model.ConsensusTrace{
			Rounds:   trace.Rounds,
			Decideds: trace.Decideds,
		},
		SyncCommittee: trace.SyncCommittee,
		Attester:      trace.Attester,
		OperatorIDs:   trace.OperatorIDs,
	}, nil
}

func (a *adapter) GetAllValidatorDuties(role spectypes.BeaconRole, slot phase0.Slot) ([]*model.ValidatorDutyTrace, error) {
	panic("not implemented")
}

// noOpTracer is a placeholder tracer, no meant to be called.
type noOpTracer struct{}

func NoOp() *noOpTracer {
	return new(noOpTracer)
}

func (n *noOpTracer) Trace(*queue.SSVMessage) {}
func (n *noOpTracer) Store() DutyTraceStore {
	return new(noOpStore)
}

type noOpStore struct{}

func (n *noOpStore) GetValidatorDuty(spectypes.BeaconRole, phase0.Slot, phase0.ValidatorIndex) (*model.ValidatorDutyTrace, error) {
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
