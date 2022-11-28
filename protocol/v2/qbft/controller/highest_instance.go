package controller

import (
	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
	"github.com/pkg/errors"
)

func (c *Controller) GetHighestInstance(identifier []byte) (*instance.Instance, error) {
	state, err := c.config.GetStorage().GetHighestInstance(identifier)
	if err != nil {
		return nil, errors.Wrap(err, "could not fetch highest instance")
	}
	if state == nil {
		return nil, nil
	}
	return instance.NewInstance(
		c.config,
		state.Share,
		identifier,
		state.Height,
	), nil
}

func (c *Controller) SaveHighestInstance(instance *instance.Instance) error {
	return c.config.GetStorage().SaveHighestInstance(instance.State)
}
