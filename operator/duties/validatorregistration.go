package duties

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
)

const validatorRegistrationEpochInterval = uint64(10)

type ValidatorRegistrationHandler struct {
	baseHandler

	validatorsPassedFirstRegistration map[string]struct{}
}

func NewValidatorRegistrationHandler() *ValidatorRegistrationHandler {
	return &ValidatorRegistrationHandler{
		validatorsPassedFirstRegistration: map[string]struct{}{},
	}
}

func (h *ValidatorRegistrationHandler) Name() string {
	return spectypes.BNRoleValidatorRegistration.String()
}

func (h *ValidatorRegistrationHandler) HandleDuties(ctx context.Context) {
	h.logger.Info("starting duty handler")

	for {
		select {
		case <-ctx.Done():
			return

		case slot := <-h.ticker:
			shares := h.validatorController.GetOperatorShares()

			sent := 0
			for _, share := range shares {
				if !share.HasBeaconMetadata() {
					continue
				}

				// if not passed first registration, should be registered within one epoch time in a corresponding slot
				// if passed first registration, should be registered within validatorRegistrationEpochInterval epochs time in a corresponding slot
				registrationSlotInterval := h.network.SlotsPerEpoch()
				if _, ok := h.validatorsPassedFirstRegistration[string(share.ValidatorPubKey)]; ok {
					registrationSlotInterval *= validatorRegistrationEpochInterval
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

				sent++
				h.validatorsPassedFirstRegistration[string(share.ValidatorPubKey)] = struct{}{}
			}
			h.logger.Debug("validator registration duties sent", zap.Uint64("slot", uint64(slot)), fields.Count(sent))
		}
	}
}
