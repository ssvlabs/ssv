package instance

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/pkg/errors"
)

func GetHighestInstance(identifier []byte, config types.IConfig) (*Instance, error) {
	state, err := config.GetStorage().GetHighestInstance(identifier)
	if  err != nil {
		return nil, errors.Wrap(err, "could not fetch instance")
	}
	if state == nil {
		return nil, nil
	}
	return &Instance{
		State:       state,
		config:      config,
		processMsgF: spectypes.NewThreadSafeF(),
	}, nil
}
