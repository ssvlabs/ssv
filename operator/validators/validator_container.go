package validators

import (
	"errors"
	"sync"
	"sync/atomic"

	genesisvalidator "github.com/ssvlabs/ssv/protocol/genesis/ssv/validator"
	genesistypes "github.com/ssvlabs/ssv/protocol/genesis/types"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"go.uber.org/zap"
)

type ValidatorContainer struct {
	validator        atomic.Pointer[validator.Validator]
	genesisValidator atomic.Pointer[genesisvalidator.Validator]
	mtx              sync.Mutex
}

func NewValidatorContainer(validator *validator.Validator, genesisValidator *genesisvalidator.Validator) (*ValidatorContainer, error) {
	if validator == nil {
		return nil, errors.New("validator must not be nil")
	}
	vc := &ValidatorContainer{}
	vc.validator.Store(validator)
	vc.genesisValidator.Store(genesisValidator)
	return vc, nil
}

func (vc *ValidatorContainer) Validator() *validator.Validator {
	return vc.validator.Load()
}

func (vc *ValidatorContainer) GenesisValidator() (*genesisvalidator.Validator, bool) {
	v := vc.genesisValidator.Load()
	if v == nil {
		return nil, false
	}
	return v, true
}

func (vc *ValidatorContainer) UnsetGenesisValidator() {
	vc.genesisValidator.Store(nil)
}

func (vc *ValidatorContainer) Start(logger *zap.Logger) (started bool, err error) {
	started, err = vc.Validator().Start(logger)
	if !started || err != nil {
		return
	}
	if v, ok := vc.GenesisValidator(); ok {
		started, err = v.Start(logger)
	}
	return
}

func (vc *ValidatorContainer) Stop() {
	if v := vc.Validator(); v != nil {
		v.Stop()
	}
	if v, ok := vc.GenesisValidator(); ok {
		v.Stop()
	}
}

func (vc *ValidatorContainer) Share() *types.SSVShare {
	return vc.Validator().Share
}

func (vc *ValidatorContainer) UpdateShare(updateAlan func(*types.SSVShare), updateGenesis func(*genesistypes.SSVShare)) {
	if updateAlan == nil || updateGenesis == nil {
		panic("both updateAlan and updateGenesis must be provided")
	}

	vc.mtx.Lock()
	defer vc.mtx.Unlock()

	if v := vc.Validator(); v != nil {
		updateAlan(v.Share)
	}
	if v, ok := vc.GenesisValidator(); ok {
		updateGenesis(v.Share)
	}
}
