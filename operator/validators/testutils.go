package validators

import (
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
)

// WithInitialState sets initial state
func WithInitialState(vstate map[spectypes.ValidatorPK]*validator.Validator, mstate map[spectypes.CommitteeID]*validator.Committee) Option {
	return func(vm *ValidatorsMap) {
		vm.validators = vstate
		vm.committees = mstate
	}
}
