package validators

import (
	genesisvalidator "github.com/ssvlabs/ssv/protocol/genesis/ssv/validator"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"go.uber.org/zap"
)

type ValidatorContainer struct {
	Validator        *validator.Validator
	GenesisValidator *genesisvalidator.Validator
}

func (vc *ValidatorContainer) Start(logger *zap.Logger) (started bool, err error) {
	started, err = vc.Validator.Start(logger)
	if !started || err != nil {
		return
	}
	started, err = vc.GenesisValidator.Start(logger)
	return
}

func (vc *ValidatorContainer) Stop() {
	if vc.Validator != nil {
		vc.Validator.Stop()
	}
	if vc.GenesisValidator != nil {
		vc.GenesisValidator.Stop()
	}
}

func (vc *ValidatorContainer) Share() *types.SSVShare {
	return vc.Validator.Share
}
