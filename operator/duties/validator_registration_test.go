package duties

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

// At and after the Gloas fork, validator registration is deprecated, so processExecution emits
// nothing (validatorProvider is nil here: it would panic if the gate failed to short-circuit).
func TestValidatorRegistrationHandler_processExecution_skippedAtGloas(t *testing.T) {
	const gloasEpoch = 100
	netCfg := networkconfig.TestNetworkWithGloas(gloasEpoch)

	executed := make(chan []*spectypes.ValidatorDuty, 1)
	h := NewValidatorRegistrationHandler(nil)
	h.logger = zap.NewNop()
	h.netCfg = netCfg
	h.dutiesExecutor = &captureExecutor{executed: executed}

	h.processExecution(context.Background(), gloasEpoch, phase0.Slot(uint64(gloasEpoch)*netCfg.SlotsPerEpoch))

	require.Len(t, executed, 0)
}

func TestValidatorRegistrationHandler_HandleDuties(t *testing.T) {
	t.Run("duty triggered by ticker", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			regCh := make(chan RegistrationDescriptor)
			handler := NewValidatorRegistrationHandler(regCh)

			// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
			// This deadline needs to be large enough to not prevent tests from executing their intended flow.
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
			defer cancel()

			// Set genesis time far enough in the past so that small block numbers
			// (used as seconds-since-epoch in test headers) are always after genesis.
			//
			// Ensure genesis is not in the future relative to mocked block timestamps (1,2,5... seconds).
			//
			// Use 1-second slots so that block number == slot in the test’s 1:1 mapping assertion.
			scheduler, ticker := setupSchedulerAndMocksWithParams(ctx, t, []dutyHandler{handler}, time.Unix(0, 0), time.Second)

			require.NoError(t, scheduler.Start(ctx))

			blockByNumberCalls := create1to1BlockSlotMapping(scheduler)
			assert1to1BlockSlotMapping(t, scheduler)
			require.EqualValues(t, 1, blockByNumberCalls.Load())

			const slot = phase0.Slot(1)

			validatorIndex1 := phase0.ValidatorIndex(1)
			validatorPk1 := phase0.BLSPubKey{1, 2, 3}
			validatorIndex2 := phase0.ValidatorIndex(2)
			validatorPk2 := phase0.BLSPubKey{4, 5, 6}
			validatorIndex3 := phase0.ValidatorIndex(scheduler.netCfg.SlotsPerEpoch*frequencyEpochs + 1)
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

			close(regCh)
			require.NoError(t, scheduler.Wait())
			ticker.WaitShutdown()
		})
	})

	t.Run("duty triggered via validatorRegistrationCh", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			regCh := make(chan RegistrationDescriptor)
			handler := NewValidatorRegistrationHandler(regCh)

			// Duty executor expects deadline to be set on the parent context (see "parent-context has no deadline set").
			// This deadline needs to be large enough to not prevent tests from executing their intended flow.
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
			defer cancel()

			// Set genesis time far enough in the past so that small block numbers
			// (used as seconds-since-epoch in test headers) are always after genesis.
			//
			// Ensure genesis is not in the future relative to mocked block timestamps (1,2,5... seconds).
			//
			// Use 1-second slots so that block number == slot in the test’s 1:1 mapping assertion.
			scheduler, ticker := setupSchedulerAndMocksWithParams(ctx, t, []dutyHandler{handler}, time.Unix(0, 0), time.Second)

			require.NoError(t, scheduler.Start(ctx))

			blockByNumberCalls := create1to1BlockSlotMapping(scheduler)
			assert1to1BlockSlotMapping(t, scheduler)
			require.EqualValues(t, 1, blockByNumberCalls.Load())

			const slot = phase0.Slot(1)

			validatorIndex := phase0.ValidatorIndex(1)
			validatorPk := phase0.BLSPubKey{1, 2, 3}

			// processExecution iterates SelfValidators for the periodic path.
			// Return nil so periodic produces no duties and the assertion below
			// observes only the event-driven duty.
			scheduler.validatorProvider.(*MockValidatorProvider).EXPECT().SelfValidators().Return(nil).AnyTimes()

			executeDutiesCall := make(chan []*spectypes.ValidatorDuty)
			setExecuteDutyFunc(scheduler, executeDutiesCall, 1)

			regCh <- RegistrationDescriptor{
				ValidatorPubkey: validatorPk,
				ValidatorIndex:  validatorIndex,
				BlockNumber:     uint64(slot),
			}

			// Event-driven registrations are now gated on
			// validatorRegistrationExecutionSlotsToPostpone — advance the
			// ticker past the gate so processExecution drains the queue.
			ticker.Send(slot + validatorRegistrationExecutionSlotsToPostpone)

			waitForDutiesExecution(t, nil, executeDutiesCall, timeout, []*spectypes.ValidatorDuty{
				{
					Type:           spectypes.BNRoleValidatorRegistration,
					PubKey:         validatorPk,
					ValidatorIndex: validatorIndex,
					// Slot is the shared duty slot — what gets signed;
					// intentionally lower than the execution-gate slot above.
					Slot: slot + validatorRegistrationDutySlotsToPostpone,
				},
			})
			require.EqualValues(t, 2, blockByNumberCalls.Load())
			close(regCh)
			require.NoError(t, scheduler.Wait())
			ticker.WaitShutdown()
		})
	})

	t.Run("event-driven duty deferred until execution gate", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			regCh := make(chan RegistrationDescriptor)
			handler := NewValidatorRegistrationHandler(regCh)

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
			defer cancel()

			scheduler, ticker := setupSchedulerAndMocksWithParams(ctx, t, []dutyHandler{handler}, time.Unix(0, 0), time.Second)

			require.NoError(t, scheduler.Start(ctx))

			create1to1BlockSlotMapping(scheduler)

			const slot = phase0.Slot(1)
			validatorIndex := phase0.ValidatorIndex(1)
			validatorPk := phase0.BLSPubKey{1, 2, 3}

			scheduler.validatorProvider.(*MockValidatorProvider).EXPECT().SelfValidators().Return(nil).AnyTimes()

			executeDutiesCall := make(chan []*spectypes.ValidatorDuty)
			setExecuteDutyFunc(scheduler, executeDutiesCall, 1)

			regCh <- RegistrationDescriptor{
				ValidatorPubkey: validatorPk,
				ValidatorIndex:  validatorIndex,
				BlockNumber:     uint64(slot),
			}

			// Tick one slot before the execution gate — handler should keep
			// the duty pending.
			ticker.Send(slot + validatorRegistrationExecutionSlotsToPostpone - 1)
			waitForNoAction(t, nil, executeDutiesCall, noActionTimeout)

			// Tick at the gate — handler should drain the queue.
			ticker.Send(slot + validatorRegistrationExecutionSlotsToPostpone)
			waitForDutiesExecution(t, nil, executeDutiesCall, timeout, []*spectypes.ValidatorDuty{
				{
					Type:           spectypes.BNRoleValidatorRegistration,
					PubKey:         validatorPk,
					ValidatorIndex: validatorIndex,
					Slot:           slot + validatorRegistrationDutySlotsToPostpone,
				},
			})

			close(regCh)
			require.NoError(t, scheduler.Wait())
			ticker.WaitShutdown()
		})
	})

	t.Run("multiple event-driven duties drained together", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			regCh := make(chan RegistrationDescriptor)
			handler := NewValidatorRegistrationHandler(regCh)

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
			defer cancel()

			scheduler, ticker := setupSchedulerAndMocksWithParams(ctx, t, []dutyHandler{handler}, time.Unix(0, 0), time.Second)

			require.NoError(t, scheduler.Start(ctx))

			create1to1BlockSlotMapping(scheduler)

			const slot = phase0.Slot(1)
			validatorIndex1 := phase0.ValidatorIndex(1)
			validatorPk1 := phase0.BLSPubKey{1, 2, 3}
			validatorIndex2 := phase0.ValidatorIndex(2)
			validatorPk2 := phase0.BLSPubKey{4, 5, 6}

			scheduler.validatorProvider.(*MockValidatorProvider).EXPECT().SelfValidators().Return(nil).AnyTimes()

			executeDutiesCall := make(chan []*spectypes.ValidatorDuty)
			setExecuteDutyFunc(scheduler, executeDutiesCall, 2)

			// Two registrations from the same block — both should end up in
			// the queue and drain together when the gate is reached.
			regCh <- RegistrationDescriptor{
				ValidatorPubkey: validatorPk1,
				ValidatorIndex:  validatorIndex1,
				BlockNumber:     uint64(slot),
			}
			regCh <- RegistrationDescriptor{
				ValidatorPubkey: validatorPk2,
				ValidatorIndex:  validatorIndex2,
				BlockNumber:     uint64(slot),
			}

			ticker.Send(slot + validatorRegistrationExecutionSlotsToPostpone)
			waitForDutiesExecution(t, nil, executeDutiesCall, timeout, []*spectypes.ValidatorDuty{
				{
					Type:           spectypes.BNRoleValidatorRegistration,
					PubKey:         validatorPk1,
					ValidatorIndex: validatorIndex1,
					Slot:           slot + validatorRegistrationDutySlotsToPostpone,
				},
				{
					Type:           spectypes.BNRoleValidatorRegistration,
					PubKey:         validatorPk2,
					ValidatorIndex: validatorIndex2,
					Slot:           slot + validatorRegistrationDutySlotsToPostpone,
				},
			})

			close(regCh)
			require.NoError(t, scheduler.Wait())
			ticker.WaitShutdown()
		})
	})
}

// TestValidatorRegistrationDutySlotPinned guards a wire-format invariant:
// the value has been 4 since this constant was introduced, and every operator
// in a cluster must compute the same signed Timestamp regardless of code
// version. Bumping the constant is a coordinated network-wide upgrade, not a
// code-cleanup change — this test turns any literal change into a visible
// diff that PR review can catch.
func TestValidatorRegistrationDutySlotPinned(t *testing.T) {
	t.Parallel()
	require.EqualValues(t, 4, validatorRegistrationDutySlotsToPostpone)
}
