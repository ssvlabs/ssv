package controller

import (
	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
	qbftstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"

	"github.com/pkg/errors"
)

func (c *Controller) LoadHighestInstance(identifier []byte) error {
	highestInstance, err := c.getHighestInstance(identifier[:])
	if err != nil {
		return err
	}
	if highestInstance == nil {
		return nil
	}
	c.Height = highestInstance.GetHeight()
	c.StoredInstances = InstanceContainer{
		0: highestInstance,
	}
	return nil
}

func (c *Controller) getHighestInstance(identifier []byte) (*instance.Instance, error) {
	highestInstance, err := c.config.GetStorage().GetHighestInstance(identifier)
	if err != nil {
		return nil, errors.Wrap(err, "could not fetch highest instance")
	}
	if highestInstance == nil {
		return nil, nil
	}
	return instance.NewInstance(
		c.config,
		highestInstance.State.Share,
		identifier,
		highestInstance.State.Height,
	), nil
}

func (c *Controller) SaveHighestInstance(instance *qbftstorage.StoredInstance) error {
	return c.config.GetStorage().SaveHighestInstance(instance)
}
