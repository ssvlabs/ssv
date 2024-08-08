package validators

import (
	"sync"

	genesisvalidator "github.com/ssvlabs/ssv/protocol/genesis/ssv/validator"
	genesistypes "github.com/ssvlabs/ssv/protocol/genesis/types"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"go.uber.org/zap"
)

type ValidatorContainer struct {
	Validator        *validator.Validator
	GenesisValidator *genesisvalidator.Validator
	mtx              sync.Mutex
}

func (vc *ValidatorContainer) Start(logger *zap.Logger, isForked func() bool) (started bool, err error) {
	started, err = vc.Validator.Start(logger)
	if !started || err != nil {
		return
	}
	if vc.GenesisValidator != nil && !isForked() {
		started, err = vc.GenesisValidator.Start(logger)
	}
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

func (vc *ValidatorContainer) UpdateShare(updateAlan func(*types.SSVShare), updateGenesis func(*genesistypes.SSVShare)) {
	if updateAlan == nil || updateGenesis == nil {
		panic("both updateAlan and updateGenesis must be provided")
	}

	vc.mtx.Lock()
	defer vc.mtx.Unlock()

	if vc.Validator != nil {
		updateAlan(vc.Validator.Share)
	}
	if vc.GenesisValidator != nil {
		updateGenesis(vc.GenesisValidator.Share)
	}
}
