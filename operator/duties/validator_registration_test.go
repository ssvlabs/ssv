package duties

import (
	"testing"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/types"
)

func TestValidatorRegistrationHandler_HandleDuties(t *testing.T) {
	t.Run("duty triggered by ticker", func(t *testing.T) {
		regCh := make(chan RegistrationDescriptor)
		handler := NewValidatorRegistrationHandler(regCh)

		currentSlot := &SafeValue[phase0.Slot]{}
		currentSlot.Set(0)

		scheduler, logger, ticker, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{handler}, currentSlot)
		startFn()
		defer func() {
			cancel()
			close(regCh)
			require.NoError(t, schedulerPool.Wait())
		}()

		blockByNumberCalls := create1to1BlockSlotMapping(scheduler)
		assert1to1BlockSlotMapping(t, scheduler)
		require.EqualValues(t, 1, blockByNumberCalls.Load())

		const slot = 1

		validatorIndex1 := phase0.ValidatorIndex(1)
		validatorPk1 := phase0.BLSPubKey{1, 2, 3}
		validatorIndex2 := phase0.ValidatorIndex(2)
		validatorPk2 := phase0.BLSPubKey{4, 5, 6}
		validatorIndex3 := phase0.ValidatorIndex(scheduler.beaconConfig.GetSlotsPerEpoch()*frequencyEpochs + 1)
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

		currentSlot.Set(slot)
		ticker.Send(currentSlot.Get())

		waitForDutiesExecution(t, logger, nil, executeDutiesCall, timeout, []*spectypes.ValidatorDuty{
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
		regCh := make(chan RegistrationDescriptor)
		handler := NewValidatorRegistrationHandler(regCh)

		currentSlot := &SafeValue[phase0.Slot]{}
		currentSlot.Set(0)

		scheduler, logger, _, timeout, cancel, schedulerPool, startFn := setupSchedulerAndMocks(t, []dutyHandler{handler}, currentSlot)
		startFn()
		defer func() {
			cancel()
			close(regCh)
			require.NoError(t, schedulerPool.Wait())
		}()

		blockByNumberCalls := create1to1BlockSlotMapping(scheduler)
		assert1to1BlockSlotMapping(t, scheduler)
		require.EqualValues(t, 1, blockByNumberCalls.Load())

		const slot = 1

		validatorIndex := phase0.ValidatorIndex(1)
		validatorPk := phase0.BLSPubKey{1, 2, 3}

		executeDutiesCall := make(chan []*spectypes.ValidatorDuty)
		setExecuteDutyFunc(scheduler, executeDutiesCall, 1)

		regCh <- RegistrationDescriptor{
			ValidatorPubkey: validatorPk,
			ValidatorIndex:  validatorIndex,
			BlockNumber:     slot,
		}

		waitForDutiesExecution(t, logger, nil, executeDutiesCall, timeout, []*spectypes.ValidatorDuty{
			{
				Type:           spectypes.BNRoleValidatorRegistration,
				PubKey:         validatorPk,
				ValidatorIndex: validatorIndex,
				Slot:           slot + validatorRegistrationSlotsToPostpone,
			},
		})
		require.EqualValues(t, 2, blockByNumberCalls.Load())
	})
}
