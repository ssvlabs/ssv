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

	if err := gc.fetchAttesterDuties(duties, epoch, validatorIndices); err != nil {
		gc.logger.Warn("failed to get attestation duties", zap.Error(err))
	}
	if err := gc.fetchProposerDuties(duties, epoch, validatorIndices); err != nil {
		gc.logger.Warn("failed to get proposer duties", zap.Error(err))
	}
	if err := gc.fetchSyncCommitteeDuties(duties, epoch, validatorIndices); err != nil {
		gc.logger.Warn("failed to get sync committee duties", zap.Error(err))
	}
	return duties, nil
}

// fetchAttesterDuties applies attester + aggregator duties
func (gc *goClient) fetchAttesterDuties(duties []*spectypes.Duty, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) error {
	if provider, isProvider := gc.client.(eth2client.AttesterDutiesProvider); isProvider {
		attesterDuties, err := provider.AttesterDuties(gc.ctx, epoch, validatorIndices)
		if err != nil {
			return err
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
	} else {
		// print log?
	}
	return nil
}

// fetchProposerDuties applies proposer duties
func (gc *goClient) fetchProposerDuties(duties []*spectypes.Duty, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) error {
	if provider, isProvider := gc.client.(eth2client.ProposerDutiesProvider); isProvider {
		proposerDuties, err := provider.ProposerDuties(gc.ctx, epoch, validatorIndices)
		if err != nil {
			return err
		}
		for _, proposerDuty := range proposerDuties {
			duties = append(duties, &spectypes.Duty{
				Type:           spectypes.BNRoleProposer,
				PubKey:         proposerDuty.PubKey,
				Slot:           proposerDuty.Slot,
				ValidatorIndex: proposerDuty.ValidatorIndex,
			})
		}
	} else {
		// print log?
	}
	return nil
}

// fetchSyncCommitteeDuties applies sync committee + sync committee contributor duties
func (gc *goClient) fetchSyncCommitteeDuties(duties []*spectypes.Duty, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) error {
	if provider, isProvider := gc.client.(eth2client.SyncCommitteeDutiesProvider); isProvider {
		syncCommitteeDuties, err := provider.SyncCommitteeDuties(gc.ctx, epoch, validatorIndices)
		if err != nil {
			return err
		}
		toBeaconDuty := func(duty *api.SyncCommitteeDuty, role spectypes.BeaconRole) *spectypes.Duty {
			return &spectypes.Duty{
				Type:                          role,
				PubKey:                        duty.PubKey,
				ValidatorIndex:                duty.ValidatorIndex,
				ValidatorSyncCommitteeIndices: duty.ValidatorSyncCommitteeIndices,
			}
		}
		for _, syncCommitteeDuty := range syncCommitteeDuties {
			duties = append(duties, toBeaconDuty(syncCommitteeDuty, spectypes.BNRoleSyncCommittee))
			duties = append(duties, toBeaconDuty(syncCommitteeDuty, spectypes.BNRoleSyncCommitteeContribution)) // always trigger contributor as well
		}
	} else {
		// print log?
	}
	return nil
}
