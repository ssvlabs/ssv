package controller

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
	qbftstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	"go.uber.org/zap"

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
	c.StoredInstances.reset()
	c.StoredInstances.addNewInstance(highestInstance)
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
	i := instance.NewInstance(
		c.config,
		highestInstance.State.Share,
		identifier,
		highestInstance.State.Height,
	)
	i.State = highestInstance.State
	return i, nil
}

func (c *Controller) SaveInstance(i *instance.Instance, msg *specqbft.SignedMessage) error {
	storedInstance := &qbftstorage.StoredInstance{
		State:          i.State,
		DecidedMessage: msg,
	}
	c.logger.Debug("TestTest saving decided instance",
		zap.Uint64("height", uint64(msg.Message.Height)), zap.Any("signers", msg.Signers), zap.Int("type", int(msg.Message.MsgType)), zap.Stack("stack"))
	return c.config.GetStorage().SaveInstance(storedInstance)
}
