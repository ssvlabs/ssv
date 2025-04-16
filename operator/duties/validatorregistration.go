package duties

import (
	"context"
	"encoding/hex"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"
)

// frequencyEpochs defines how frequently we want to submit validator-registrations.
const frequencyEpochs = uint64(10)

type ValidatorRegistrationHandler struct {
	baseHandler
}

type ValidatorRegistration struct {
	ValidatorIndex phase0.ValidatorIndex
	FeeRecipient   string
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

			var vrs []ValidatorRegistration
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
					// no need for other params
				}})

				vrs = append(vrs, ValidatorRegistration{
					ValidatorIndex: share.ValidatorIndex,
					FeeRecipient:   hex.EncodeToString(share.FeeRecipientAddress[:]),
				})
			}
			h.logger.Debug("validator registration duties sent",
				zap.Uint64("slot", uint64(slot)),
				zap.Any("validator_registrations", vrs))

		case <-h.indicesChange:
			continue

		case <-h.reorg:
			continue
		}
	}
}
