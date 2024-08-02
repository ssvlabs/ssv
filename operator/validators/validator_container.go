package validators

import (
	genesisvalidator "github.com/ssvlabs/ssv/protocol/genesis/ssv/validator"
	genesistypes "github.com/ssvlabs/ssv/protocol/genesis/types"
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

func (vc *ValidatorContainer) UpdateValidatorShare(updateFunc func(*types.SSVShare)) {
	if vc.Validator != nil {
		updateFunc(vc.Validator.Share)
	}
}

func (vc *ValidatorContainer) UpdateGenesisShare(updateFunc func(*genesistypes.SSVShare)) {
	if vc.GenesisValidator != nil {
		updateFunc(vc.GenesisValidator.Share)
	}
}
