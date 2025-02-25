package validator

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	model "github.com/ssvlabs/ssv/exporter/v2"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
)

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
	// TODO(me): deep copy (also in committee below)
	return traces, nil
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

func (a *adapter) committeeDecideds(slot phase0.Slot, pubKeys []spectypes.ValidatorPK) (out []qbftstorage.ParticipantsRangeEntry) {
	return nil
}

func (a *adapter) GetDecideds(role spectypes.BeaconRole, slot phase0.Slot, pubkeys []spectypes.ValidatorPK) (out []qbftstorage.ParticipantsRangeEntry) {
	switch role {
	case spectypes.BNRoleSyncCommittee:
		fallthrough
	case spectypes.BNRoleAttester:
		out = append(out, a.committeeDecideds(slot, pubkeys)...)
		return
	default:
		for _, pubkey := range pubkeys {
			duty, err := a.tracer.getValidatorDuty(role, slot, pubkey)
			if err != nil {
				continue
			}

			var signers []spectypes.OperatorID

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
