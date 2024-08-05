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

func (vc *ValidatorContainer) UpdateShare(updateFunc func(*genesistypes.SSVShare, *types.SSVShare)) {
	vc.mtx.Lock()
	defer vc.mtx.Unlock()
	switch {
	case vc.GenesisValidator != nil && vc.Validator != nil:
		updateFunc(vc.GenesisValidator.Share, vc.Validator.Share)
	case vc.GenesisValidator != nil:
		updateFunc(vc.GenesisValidator.Share, nil)
	case vc.Validator != nil:
		updateFunc(nil, vc.Validator.Share)
	default:
		updateFunc(nil, nil)
	}
}
