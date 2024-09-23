package duties

import (
	"context"

	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
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
	defer h.logger.Info("duty handler exited")

	// should be registered within validatorRegistrationEpochInterval epochs time in a corresponding slot
	registrationSlotInterval := h.network.SlotsPerEpoch() * validatorRegistrationEpochInterval

	next := h.ticker.Next()
	for {
		select {
		case <-ctx.Done():
			return

		case <-next:
			slot := h.ticker.Slot()
			next = h.ticker.Next()
			epoch := h.network.Beacon.EstimatedEpochAtSlot(slot)
			shares := h.validatorProvider.SelfParticipatingValidators(epoch + phase0.Epoch(validatorRegistrationEpochInterval))

			var validators []phase0.ValidatorIndex
			for _, share := range shares {
				if uint64(share.BeaconMetadata.Index)%registrationSlotInterval != uint64(slot)%registrationSlotInterval {
					continue
				}

				pk := phase0.BLSPubKey{}
				copy(pk[:], share.ValidatorPubKey[:])
				if !h.network.PastAlanForkAtEpoch(epoch) {
					h.dutiesExecutor.ExecuteGenesisDuties(h.logger, []*genesisspectypes.Duty{{
						Type:           genesisspectypes.BNRoleValidatorRegistration,
						ValidatorIndex: share.ValidatorIndex,
						PubKey:         pk,
						Slot:           slot,
						// no need for other params
					}})
				} else {
					h.dutiesExecutor.ExecuteDuties(h.logger, []*spectypes.ValidatorDuty{{
						Type:           spectypes.BNRoleValidatorRegistration,
						ValidatorIndex: share.ValidatorIndex,
						PubKey:         pk,
						Slot:           slot,
						// no need for other params
					}})
				}

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
