package controller

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/pkg/errors"
)

// OnTimeout is trigger upon timeout for the given height
func (c *Controller) OnTimeout(msg *specqbft.SignedMessage) error {
	// TODO add validation

	instance := c.StoredInstances.FindInstance(msg.Message.Height)
	if instance == nil {
		return errors.New("instance is nil")
	}
	decided, _ := instance.IsDecided()
	if decided {
		return errors.New("instance already decided")
	}
	return instance.UponRoundTimeout()
}
