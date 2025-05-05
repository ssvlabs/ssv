package duties

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"
)

// frequencyEpochs defines how frequently we want to submit validator-registrations.
const frequencyEpochs = uint64(10)

type ValidatorRegistrationHandler struct {
	baseHandler
}

func NewValidatorRegistrationHandler() *ValidatorRegistrationHandler {
	return &ValidatorRegistrationHandler{}
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
	registrationSlots := h.network.SlotsPerEpoch() * frequencyEpochs

	next := h.ticker.Next()
	for {
		select {
		case <-ctx.Done():
			return

		case <-next:
			slot := h.ticker.Slot()
			next = h.ticker.Next()
			epoch := h.network.Beacon.EstimatedEpochAtSlot(slot)
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
				h.dutiesExecutor.ExecuteDuties(ctx, h.logger, []*spectypes.ValidatorDuty{{
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

		case <-h.indicesChange:
			continue

		case <-h.reorg:
			continue
		}
	}
}
