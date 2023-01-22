package controller

import (
	"github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/pkg/errors"
)

// OnTimeout is trigger upon timeout for the given height
func (c *Controller) OnTimeout(msg types.EventMsg) error {
	// TODO add validation

	timeoutData, err := msg.GetTimeoutData()
	if err != nil {
		return errors.Wrap(err, "failed to get timeout data")
	}
	instance := c.StoredInstances.FindInstance(timeoutData.Height)
	if instance == nil {
		return errors.New("instance is nil")
	}
	decided, _ := instance.IsDecided()
	if decided {
		return nil
	}
	return instance.UponRoundTimeout()
}
