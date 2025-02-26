package validator

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	model "github.com/ssvlabs/ssv/exporter/v2"
	"github.com/ssvlabs/ssv/logging/fields"
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

func (a *adapter) getCommitteDecidedsFromDisk(slot phase0.Slot, pubkeys []spectypes.ValidatorPK) (out []qbftstorage.ParticipantsRangeEntry, err error) {
	for _, pubkey := range pubkeys {
		index, found := a.tracer.validators.ValidatorIndex(pubkey)
		if !found {
			continue
		}
		committeeID, err := a.tracer.store.GetCommitteeDutyLink(slot, index)
		if err != nil {
			return nil, err
		}
		duty, err := a.tracer.store.GetCommitteeDuty(slot, committeeID)
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

	return out, nil
}

func (a *adapter) committeeDecideds(slot phase0.Slot, pubkeys []spectypes.ValidatorPK) (out []qbftstorage.ParticipantsRangeEntry, err error) {
	var slotData *ttlcache.Item[phase0.Slot, map[phase0.ValidatorIndex]spectypes.CommitteeID]

	func() {
		a.tracer.validatorCommitteeMappingMu.Lock()
		defer a.tracer.validatorCommitteeMappingMu.Unlock()

		slotData = a.tracer.validatorCommitteeMapping.Get(slot)
	}()

	if slotData == nil { // no mapping found, get from disk
		out, err := a.getCommitteDecidedsFromDisk(slot, pubkeys)
		if err != nil {
			return nil, fmt.Errorf("get committee decideds from disk: %w", err)
		}
		return out, nil
	}

	a.tracer.validatorCommitteeMappingMu.Lock()
	defer a.tracer.validatorCommitteeMappingMu.Unlock()

	// iterate over pubkeys
	for _, pubkey := range pubkeys {
		index, found := a.tracer.validators.ValidatorIndex(pubkey)
		if !found {
			continue
		}

		committeeID, found := slotData.Value()[index]
		if !found {
			continue
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
