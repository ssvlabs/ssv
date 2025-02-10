package operator

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/api/handlers"
	model "github.com/ssvlabs/ssv/exporter/v2"
	"github.com/ssvlabs/ssv/operator/validator"
	"go.uber.org/zap"
)

// tmp hack

func traceStoreAdapter(tracer *validator.InMemTracer, logger *zap.Logger) handlers.DutyTraceStore {
	return &adapter{
		tracer: tracer,
		logger: logger,
	}
}

type adapter struct {
	tracer *validator.InMemTracer
	logger *zap.Logger
}

func (a *adapter) GetValidatorDuty(role spectypes.BeaconRole, slot phase0.Slot, pubkey spectypes.ValidatorPK) (*model.ValidatorDutyTrace, error) {
	trace, err := a.tracer.GetValidatorDuty(role, slot, pubkey)
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
	trace, err := a.tracer.GetCommitteeDuty(slot, committeeID)
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
		Post:        trace.Post,
		OperatorIDs: trace.OperatorIDs,
	}, nil
}

func (a *adapter) GetAllValidatorDuties(role spectypes.BeaconRole, slot phase0.Slot) ([]*model.ValidatorDutyTrace, error) {
	panic("not implemented")
}
