package duties

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	"go.uber.org/zap"
)

const validatorRegistrationEpochInterval = uint64(10)

type ValidatorRegistrationHandler struct {
	baseHandler
}

func NewValidatorRegistrationHandler() *ValidatorRegistrationHandler {
	return &ValidatorRegistrationHandler{}
}

func (h *ValidatorRegistrationHandler) Name() string {
	return spectypes.BNRoleValidatorRegistration.String()
}

func (h *ValidatorRegistrationHandler) HandleDuties(ctx context.Context) {
	h.logger.Info("starting duty handler")

	// should be registered within validatorRegistrationEpochInterval epochs time in a corresponding slot
	registrationSlotInterval := h.network.SlotsPerEpoch() * validatorRegistrationEpochInterval

	for {
		select {
		case <-ctx.Done():
			return

		case <-h.ticker.Next():
			slot := h.ticker.Slot()
			epoch := h.network.Beacon.EstimatedEpochAtSlot(slot)
			shares := h.validatorController.GetOperatorShares()

			var validators []phase0.ValidatorIndex
			for _, share := range shares {
				if !share.IsAttesting(epoch + phase0.Epoch(validatorRegistrationEpochInterval)) {
					continue
				}

				if uint64(share.BeaconMetadata.Index)%registrationSlotInterval != uint64(slot)%registrationSlotInterval {
					continue
				}

				pk := phase0.BLSPubKey{}
				copy(pk[:], share.ValidatorPubKey)

				h.executeDuties(h.logger, []*spectypes.Duty{{
					Type:   spectypes.BNRoleValidatorRegistration,
					PubKey: pk,
					Slot:   slot,
					// no need for other params
				}})

				validators = append(validators, share.BeaconMetadata.Index)
			}
			h.logger.Debug("validator registration duties sent",
				zap.Uint64("slot", uint64(slot)),
				zap.Any("validators", validators))

		case <-h.indicesChange:
			continue

		case <-h.reorg:
			continue
		}
	}
}
