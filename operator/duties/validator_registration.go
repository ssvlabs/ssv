package duties

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"
)

// frequencyEpochs defines how frequently we want to submit validator-registrations.
const frequencyEpochs = 10

type RegistrationDescriptor struct {
	ValidatorIndex  phase0.ValidatorIndex
	ValidatorPubkey phase0.BLSPubKey
	FeeRecipient    []byte
	BlockNumber     uint64
}

type ValidatorRegistrationHandler struct {
	baseHandler
	validatorRegCh <-chan RegistrationDescriptor
	blockSlots     map[uint64]phase0.Slot
}

func NewValidatorRegistrationHandler(validatorRegistrationCh <-chan RegistrationDescriptor) *ValidatorRegistrationHandler {
	return &ValidatorRegistrationHandler{
		validatorRegCh: validatorRegistrationCh,
		blockSlots:     map[uint64]phase0.Slot{},
	}
}

func (h *ValidatorRegistrationHandler) Name() string {
	return spectypes.BNRoleValidatorRegistration.String()
}

// HandleDuties generates registration duties every N epochs for every participating validator, then
// validator-registrations are aggregated into batches and sent periodically to Beacon node by
// ValidatorRegistrationRunner (sending validator-registrations periodically ensures various
// entities in Ethereum network, such as Relays, are aware of participating validators).
func (h *ValidatorRegistrationHandler) HandleDuties(ctx context.Context) {
	h.logger.Info("starting duty handler")
	defer h.logger.Info("duty handler exited")

	// validator should be registered within frequencyEpochs epochs time in a corresponding slot
	registrationSlots := h.beaconConfig.GetSlotsPerEpoch() * frequencyEpochs

	next := h.ticker.Next()
	for {
		select {
		case <-ctx.Done():
			return

		case <-next:
			slot := h.ticker.Slot()
			next = h.ticker.Next()
			epoch := h.beaconConfig.EstimatedEpochAtSlot(slot)
			shares := h.validatorProvider.SelfValidators()

			for _, share := range shares {
				if !share.IsParticipatingAndAttesting(epoch + phase0.Epoch(frequencyEpochs)) {
					// Only attesting validators are eligible for registration duties.
					continue
				}
				if uint64(share.ValidatorIndex)%registrationSlots != uint64(slot)%registrationSlots {
					continue
				}

				pk := phase0.BLSPubKey{}
				copy(pk[:], share.ValidatorPubKey[:])
				h.dutiesExecutor.ExecuteDuties(ctx, []*spectypes.ValidatorDuty{{
					Type:           spectypes.BNRoleValidatorRegistration,
					ValidatorIndex: share.ValidatorIndex,
					PubKey:         pk,
					Slot:           slot,
				}})

				h.logger.Debug("validator registration duty sent",
					zap.Uint64("slot", uint64(slot)),
					zap.Uint64("validator_index", uint64(share.ValidatorIndex)),
					zap.String("validator_pubkey", pk.String()))
			}

		case regDescriptor, ok := <-h.validatorRegCh:
			if !ok {
				return
			}

			// Calculate duty slot in a deterministic manner to ensure every Operator will have the same
			// slot value for this duty. Additionally, add validatorRegistrationSlotsToPostpone slots on
			// top to ensure the duty is scheduled with a slot number never in the past since several slots
			// might have passed by the time we are processing this event here.
			const validatorRegistrationSlotsToPostpone = phase0.Slot(4)
			blockSlot, err := h.blockSlot(ctx, regDescriptor.BlockNumber)
			if err != nil {
				h.logger.Warn(
					"failed to convert block number to slot number, skipping validator registration duty",
					zap.Error(err),
				)
				continue
			}
			dutySlot := blockSlot + validatorRegistrationSlotsToPostpone

			// Kick off validator registration duty to notify various Ethereum actors (e.g. Relays)
			// about fee recipient change as soon as possible.
			h.dutiesExecutor.ExecuteDuties(ctx, []*spectypes.ValidatorDuty{{
				Type:           spectypes.BNRoleValidatorRegistration,
				ValidatorIndex: regDescriptor.ValidatorIndex,
				PubKey:         regDescriptor.ValidatorPubkey,
				Slot:           dutySlot,
			}})
			h.logger.Debug("validator registration duty sent",
				zap.Uint64("slot", uint64(dutySlot)),
				zap.Uint64("validator_index", uint64(regDescriptor.ValidatorIndex)),
				zap.String("validator_pubkey", regDescriptor.ValidatorPubkey.String()),
				zap.String("validator_fee_recipient", hex.EncodeToString(regDescriptor.FeeRecipient[:])))

		case <-h.indicesChange:
			h.logger.Debug("🛠 indicesChange event")

		case <-h.reorg:
			h.logger.Debug("🛠 reorg event")
		}
	}
}

// blockSlot returns slot that happens (corresponds to) at the same time as block.
func (h *ValidatorRegistrationHandler) blockSlot(ctx context.Context, blockNumber uint64) (phase0.Slot, error) {
	blockSlot, ok := h.blockSlots[blockNumber]
	if ok {
		return blockSlot, nil
	}

	block, err := h.executionClient.BlockByNumber(ctx, new(big.Int).SetUint64(blockNumber))
	if err != nil {
		return 0, fmt.Errorf("request block %d from execution client: %w", blockNumber, err)
	}

	blockSlot = h.beaconConfig.EstimatedSlotAtTime(time.Unix(int64(block.Time()), 0)) // #nosec G115

	h.blockSlots[blockNumber] = blockSlot

	// Clean up older cached values since they are not relevant anymore.
	for k, v := range h.blockSlots {
		const recentlyQueriedBlocks = 10
		if blockSlot >= v+recentlyQueriedBlocks {
			delete(h.blockSlots, k)
		}
	}

	return blockSlot, nil
}
