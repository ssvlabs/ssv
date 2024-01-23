package validatormanager

import (
	"encoding/hex"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"

	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"
)

type nonCommitteeValidator struct {
	*validator.NonCommitteeValidator
	sync.Mutex
}

var nonCommitteeValidatorTTLs = map[spectypes.BeaconRole]phase0.Slot{
	spectypes.BNRoleAttester:                  64,
	spectypes.BNRoleProposer:                  4,
	spectypes.BNRoleAggregator:                4,
	spectypes.BNRoleSyncCommittee:             4,
	spectypes.BNRoleSyncCommitteeContribution: 4,
}

func (vm *ValidatorManager) HandleNonCommitteeValidatorMessage(msg *queue.DecodedSSVMessage) error {
	// Get or create a nonCommitteeValidator for this MessageID, and lock it to prevent
	// other handlers from processing
	var ncv *nonCommitteeValidator
	err := func() error {
		vm.nonCommitteeMutex.Lock()
		defer vm.nonCommitteeMutex.Unlock()

		item := vm.nonCommitteeValidators.Get(msg.GetID())
		if item != nil {
			ncv = item.Value()
		} else {
			// Create a new nonCommitteeValidator and cache it.
			share := vm.nodeStorage.Shares().Get(nil, msg.GetID().GetPubKey())
			if share == nil {
				return errors.Errorf("could not find validator [%s]", hex.EncodeToString(msg.GetID().GetPubKey()))
			}

			opts := vm.validatorOptions
			opts.SSVShare = share
			ncv = &nonCommitteeValidator{
				NonCommitteeValidator: validator.NewNonCommitteeValidator(vm.logger, msg.GetID(), opts),
			}

			ttlSlots := nonCommitteeValidatorTTLs[msg.MsgID.GetRoleType()]
			vm.nonCommitteeValidators.Set(
				msg.GetID(),
				ncv,
				time.Duration(ttlSlots)*vm.networkConfig.SlotDurationSec(),
			)
		}

		ncv.Lock()
		return nil
	}()
	if err != nil {
		return err
	}

	defer ncv.Unlock()
	ncv.ProcessMessage(vm.logger, msg)

	return nil
}
