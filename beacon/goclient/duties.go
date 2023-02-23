package goclient

import (
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/logging"
)

func (gc *goClient) GetDuties(epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*spectypes.Duty, error) {
	type FetchFunc func(epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*spectypes.Duty, error)

	fetchers := map[spectypes.BeaconRole]FetchFunc{
		spectypes.BNRoleAttester: gc.AttesterDuties,
		spectypes.BNRoleProposer: gc.ProposerDuties,
	}
	duties := make([]*spectypes.Duty, 0)
	var lock sync.Mutex
	var wg sync.WaitGroup

	start := time.Now()
	for role, fetcher := range fetchers {
		wg.Add(1)
		go func(role spectypes.BeaconRole, fetchFunc FetchFunc) {
			defer wg.Done()
			if fetchedDuties, err := fetchFunc(epoch, validatorIndices); err == nil {
				lock.Lock()
				duties = append(duties, fetchedDuties...)
				lock.Unlock()
			} else {
				gc.logger.Warn(fmt.Sprintf("failed to get %s duties", role.String()), zap.Error(err))
			}
		}(role, fetcher)
	}
	wg.Wait()

	gc.logger.Debug("fetched duties", zap.Int("count", len(duties)), logging.DurationNano(start))
	return duties, nil
}

// AttesterDuties applies attester + aggregator duties
func (gc *goClient) AttesterDuties(epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*spectypes.Duty, error) {
	var duties []*spectypes.Duty
	attesterDuties, err := gc.client.AttesterDuties(gc.ctx, epoch, validatorIndices)
	if err != nil {
		return duties, err
	}
	toBeaconDuty := func(duty *eth2apiv1.AttesterDuty, role spectypes.BeaconRole) *spectypes.Duty {
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
}

// ProposerDuties applies proposer duties
func (gc *goClient) ProposerDuties(epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*spectypes.Duty, error) {
	var duties []*spectypes.Duty
	proposerDuties, err := gc.client.ProposerDuties(gc.ctx, epoch, validatorIndices)
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
}

// SyncCommitteeDuties applies sync committee + sync committee contributor duties
func (gc *goClient) SyncCommitteeDuties(epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.SyncCommitteeDuty, error) {
	return gc.client.SyncCommitteeDuties(gc.ctx, epoch, validatorIndices)
}
