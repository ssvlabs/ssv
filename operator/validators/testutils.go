package validators

import (
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/v2/protocol/v2/ssv/validator"
)

// Test helper to set initial state during tests.
func WithInitialState(vstate map[spectypes.ValidatorPK]*validator.Validator, mstate map[spectypes.CommitteeID]*validator.Committee) Option {
	return func(vm *ValidatorsMap) {
		vm.validators = vstate
		vm.committees = mstate
	}
}
