package controller

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/pkg/errors"

	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
	qbftstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
)

func (c *Controller) LoadHighestInstance(identifier []byte) (*instance.Instance, error) {
	highestInstance, err := c.getHighestInstance(identifier[:])
	if err != nil {
		return nil, err
	}
	if highestInstance == nil {
		return nil, nil
	}
	c.Height = highestInstance.GetHeight()
	c.StoredInstances.reset()
	c.StoredInstances.AddNewInstance(highestInstance)
	return highestInstance, nil
}

func (c *Controller) getHighestInstance(identifier []byte) (*instance.Instance, error) {
	highestInstance, err := c.config.GetStorage().GetHighestInstance(identifier)
	if err != nil {
		return nil, errors.Wrap(err, "could not fetch highest instance")
	}
	if highestInstance == nil {
		return nil, nil
	}

	// Compact the instance to reduce its memory footprint.
	instance.Compact(highestInstance.State, highestInstance.DecidedMessage)

	i := instance.NewInstance(
		c.config,
		highestInstance.State.Share,
		identifier,
		highestInstance.State.Height,
	)
	i.State = highestInstance.State
	return i, nil
}

// SaveInstance saves the given instance to the storage.
func (c *Controller) SaveInstance(i *instance.Instance, msg *specqbft.SignedMessage) error {
	storedInstance := &qbftstorage.StoredInstance{
		State:          i.State,
		DecidedMessage: msg,
	}
	isHighest := msg.Message.Height >= c.Height

	// Full nodes save both highest and historical instances.
	if c.fullNode {
		if isHighest {
			return c.config.GetStorage().SaveHighestAndHistoricalInstance(storedInstance)
		}
		return c.config.GetStorage().SaveInstance(storedInstance)
	}

	// Light nodes only save highest instances.
	if isHighest {
		return c.config.GetStorage().SaveHighestInstance(storedInstance)
	}

	return nil
}
