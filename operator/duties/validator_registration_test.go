package duties

import (
	"context"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/types"
)

func TestValidatorRegistrationHandler_HandleDuties(t *testing.T) {
	regCh := make(chan RegistrationDescriptor)
	handler := NewValidatorRegistrationHandler(regCh)

	// Duty executor expects deadline to be set on the parent context (see "failed to get parent-context deadline").
	// This deadline needs to be large enough to not prevent tests from executing their intended flow.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)

	// Set genesis time far enough in the past so that small block numbers
	// (used as seconds-since-epoch in test headers) are always after genesis.
	//
	// Ensure genesis is not in the future relative to mocked block timestamps (1,2,5... seconds).
	//
	// Use 1-second slots so that block number == slot in the test’s 1:1 mapping assertion.
	scheduler, ticker, schedulerPool := setupSchedulerAndMocksWithParams(ctx, t, []dutyHandler{handler}, time.Unix(0, 0), time.Second)

	startScheduler(ctx, t, scheduler, schedulerPool)

	blockByNumberCalls := create1to1BlockSlotMapping(scheduler)
	assert1to1BlockSlotMapping(t, scheduler)
	require.EqualValues(t, 1, blockByNumberCalls.Load())

	t.Run("duty triggered by ticker", func(t *testing.T) {
		const slot = phase0.Slot(1)

		validatorIndex1 := phase0.ValidatorIndex(1)
		validatorPk1 := phase0.BLSPubKey{1, 2, 3}
		validatorIndex2 := phase0.ValidatorIndex(2)
		validatorPk2 := phase0.BLSPubKey{4, 5, 6}
		validatorIndex3 := phase0.ValidatorIndex(scheduler.beaconConfig.SlotsPerEpoch*frequencyEpochs + 1)
		validatorPk3 := phase0.BLSPubKey{7, 8, 9}

		attestingShares := []*types.SSVShare{
			// will be eligible for validator-registration duty in slot 1
			{
				Share: spectypes.Share{
					ValidatorIndex:  validatorIndex1,
					ValidatorPubKey: spectypes.ValidatorPK(validatorPk1),
				},
				ActivationEpoch: 0,
				Liquidated:      false,
				// this particular status is needed so that ActivationEpoch can be taken into consideration when checking the IsAttesting() condition.
				Status: eth2apiv1.ValidatorStatePendingQueued,
			},

			// this validator will not be eligible for validator-registration duty in slot 1
			{
				Share: spectypes.Share{
					ValidatorIndex:  validatorIndex2,
					ValidatorPubKey: spectypes.ValidatorPK(validatorPk2),
				},
				ActivationEpoch: 0,
				Liquidated:      false,
				// this particular status is needed so that ActivationEpoch can be taken into consideration when checking the IsAttesting() condition.
				Status: eth2apiv1.ValidatorStatePendingQueued,
			},

			// this validator will not be eligible for validator-registration duty in slot 1
			{
				Share: spectypes.Share{
					ValidatorIndex:  validatorIndex3,
					ValidatorPubKey: spectypes.ValidatorPK(validatorPk3),
				},
				ActivationEpoch: 0,
				Liquidated:      true,
			},
		}
		scheduler.validatorProvider.(*MockValidatorProvider).EXPECT().SelfValidators().Return(attestingShares).AnyTimes()

		executeDutiesCall := make(chan []*spectypes.ValidatorDuty)
		setExecuteDutyFunc(scheduler, executeDutiesCall, 1)

		ticker.Send(slot)

		waitForDutiesExecution(t, nil, executeDutiesCall, timeout, []*spectypes.ValidatorDuty{
			{
				Type:           spectypes.BNRoleValidatorRegistration,
				PubKey:         validatorPk1,
				ValidatorIndex: validatorIndex1,
				Slot:           slot,
			},
		})
		require.EqualValues(t, 1, blockByNumberCalls.Load())
	})

	t.Run("duty triggered via validatorRegistrationCh", func(t *testing.T) {
		const slot = phase0.Slot(1)

		validatorIndex := phase0.ValidatorIndex(1)
		validatorPk := phase0.BLSPubKey{1, 2, 3}

		executeDutiesCall := make(chan []*spectypes.ValidatorDuty)
		setExecuteDutyFunc(scheduler, executeDutiesCall, 1)

		regCh <- RegistrationDescriptor{
			ValidatorPubkey: validatorPk,
			ValidatorIndex:  validatorIndex,
			BlockNumber:     uint64(slot),
		}

		waitForDutiesExecution(t, nil, executeDutiesCall, timeout, []*spectypes.ValidatorDuty{
			{
				Type:           spectypes.BNRoleValidatorRegistration,
				PubKey:         validatorPk,
				ValidatorIndex: validatorIndex,
				Slot:           slot + validatorRegistrationSlotsToPostpone,
			},
		})
		require.EqualValues(t, 2, blockByNumberCalls.Load())
	})

	cancel()
	close(regCh)
	require.NoError(t, schedulerPool.Wait())
}
