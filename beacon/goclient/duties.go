package goclient

import (
	eth2client "github.com/attestantio/go-eth2-client"
	api "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"
)

func (gc *goClient) GetDuties(epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*spectypes.Duty, error) {
	var duties []*spectypes.Duty

	if attesterDuties, err := gc.fetchAttesterDuties(epoch, validatorIndices); err != nil {
		gc.logger.Warn("failed to get attestation duties", zap.Error(err))
	} else {
		duties = append(duties, attesterDuties...)
	}
	if proposerDuties, err := gc.fetchProposerDuties(epoch, validatorIndices); err != nil {
		gc.logger.Warn("failed to get proposer duties", zap.Error(err))
	} else {
		duties = append(duties, proposerDuties...)
	}
	if syncCommitteeDuties, err := gc.fetchSyncCommitteeDuties(epoch, validatorIndices); err != nil {
		gc.logger.Warn("failed to get sync committee duties", zap.Error(err))
	} else {
		duties = append(duties, syncCommitteeDuties...)
	}
	return duties, nil
}

// fetchAttesterDuties applies attester + aggregator duties
func (gc *goClient) fetchAttesterDuties(epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*spectypes.Duty, error) {
	var duties []*spectypes.Duty
	if provider, isProvider := gc.client.(eth2client.AttesterDutiesProvider); isProvider {
		attesterDuties, err := provider.AttesterDuties(gc.ctx, epoch, validatorIndices)
		if err != nil {
			return duties, err
		}
		toBeaconDuty := func(duty *api.AttesterDuty, role spectypes.BeaconRole) *spectypes.Duty {
			return &spectypes.Duty{
				Type:                    role,
				PubKey:                  duty.PubKey,
				Slot:                    duty.Slot,
				ValidatorIndex:          duty.ValidatorIndex,
				CommitteeIndex:          duty.CommitteeIndex,
				CommitteeLength:         duty.CommitteeLength,
				CommitteesAtSlot:        duty.CommitteesAtSlot,
				ValidatorCommitteeIndex: duty.ValidatorCommitteeIndex,
			}
		}

		for _, attesterDuty := range attesterDuties {
			duties = append(duties, toBeaconDuty(attesterDuty, spectypes.BNRoleAttester))
			duties = append(duties, toBeaconDuty(attesterDuty, spectypes.BNRoleAggregator)) // always trigger aggregator as well
		}
		return duties, nil
	} else {
		// print log?
	}
	return duties, nil
}

// fetchProposerDuties applies proposer duties
func (gc *goClient) fetchProposerDuties(epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*spectypes.Duty, error) {
	var duties []*spectypes.Duty
	if provider, isProvider := gc.client.(eth2client.ProposerDutiesProvider); isProvider {
		proposerDuties, err := provider.ProposerDuties(gc.ctx, epoch, validatorIndices)
		if err != nil {
			return duties, err
		}
		for _, proposerDuty := range proposerDuties {
			duties = append(duties, &spectypes.Duty{
				Type:           spectypes.BNRoleProposer,
				PubKey:         proposerDuty.PubKey,
				Slot:           proposerDuty.Slot,
				ValidatorIndex: proposerDuty.ValidatorIndex,
			})
		}
		return duties, nil
	} else {
		// print log?
	}
	return duties, nil
}

// fetchSyncCommitteeDuties applies sync committee + sync committee contributor duties
func (gc *goClient) fetchSyncCommitteeDuties(epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*spectypes.Duty, error) {
	var duties []*spectypes.Duty
	if provider, isProvider := gc.client.(eth2client.SyncCommitteeDutiesProvider); isProvider {
		syncCommitteeDuties, err := provider.SyncCommitteeDuties(gc.ctx, epoch, validatorIndices)
		if err != nil {
			return duties, err
		}
		toBeaconDuty := func(duty *api.SyncCommitteeDuty, slot phase0.Slot, role spectypes.BeaconRole) *spectypes.Duty {
			return &spectypes.Duty{
				Type:                          role,
				PubKey:                        duty.PubKey,
				Slot:                          slot, // in order for the duty ctrl to execute
				ValidatorIndex:                duty.ValidatorIndex,
				ValidatorSyncCommitteeIndices: duty.ValidatorSyncCommitteeIndices,
			}
		}

		startSlot := uint64(epoch) * gc.network.SlotsPerEpoch()
		endSlot := startSlot + (gc.network.SlotsPerEpoch() - 1)
		// loop all slots in epoch and add the duties to each slot as sync committee is for each slot
		for slot := startSlot; slot <= endSlot; slot++ {
			for _, syncCommitteeDuty := range syncCommitteeDuties {
				duties = append(duties, toBeaconDuty(syncCommitteeDuty, phase0.Slot(slot), spectypes.BNRoleSyncCommittee))
				duties = append(duties, toBeaconDuty(syncCommitteeDuty, phase0.Slot(slot), spectypes.BNRoleSyncCommitteeContribution)) // always trigger contributor as well
			}
		}

		return duties, nil
	} else {
		// print log?
	}
	return duties, nil
}
