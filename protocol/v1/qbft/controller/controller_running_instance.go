package controller

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v1/sync/changeround"
)

// startInstanceWithOptions will start an iBFT instance with the provided options.
// Does not pre-check instance validity and start validity!
func (c *Controller) startInstanceWithOptions(instanceOpts *instance.Options, value []byte) (*instance.Result, error) {
	newInstance := instance.NewInstance(instanceOpts)

	c.setCurrentInstance(newInstance)

	newInstance.Init()
	stageChan := newInstance.GetStageChan()

	// reset leader seed for sequence
	if err := newInstance.Start(value); err != nil {
		return nil, errors.WithMessage(err, "could not start iBFT instance")
	}

	metricsCurrentSequence.WithLabelValues(c.Identifier.GetRoleType().String(), hex.EncodeToString(c.Identifier.GetValidatorPK())).Set(float64(newInstance.State().GetHeight()))

	// catch up if we can
	go c.fastChangeRoundCatchup(newInstance)

	// main instance callback loop
	var retRes *instance.Result
	var err error
instanceLoop:
	for {
		stage := <-stageChan
		if c.getCurrentInstance() == nil {
			c.logger.Debug("stage channel was invoked but instance is already empty", zap.Any("stage", stage))
			break instanceLoop
		}
		exit, e := c.instanceStageChange(stage)
		if e != nil {
			err = e
			break instanceLoop
		}
		if exit {
			// exited with no error means instance decided
			// fetch decided msg and return
			retMsg, e := c.decidedStrategy.GetDecided(c.Identifier, instanceOpts.Height, instanceOpts.Height)
			if e != nil {
				err = e
				break instanceLoop
			}
			if len(retMsg) == 0 {
				err = errors.Errorf("could not fetch decided msg with height %d after instance finished", instanceOpts.Height)
				break instanceLoop
			}
			retRes = &instance.Result{
				Decided: true,
				Msg:     retMsg[0],
			}
			break instanceLoop
		}
	}
	var seq message.Height
	if c.getCurrentInstance() != nil {
		// saves seq as instance will be cleared
		seq = c.getCurrentInstance().State().GetHeight()
		// when main instance loop breaks, nil current instance
		c.setCurrentInstance(nil)
	}
	c.logger.Debug("iBFT instance result loop stopped")

	c.afterInstance(seq, retRes, err)

	return retRes, err
}

// afterInstance is triggered after the instance was finished
func (c *Controller) afterInstance(height message.Height, res *instance.Result, err error) {
	// if instance was decided -> wait for late commit messages
	decided := res != nil && res.Decided
	if decided && err == nil {
		if height == message.Height(0) {
			if res.Msg == nil || res.Msg.Message == nil {
				// missing sequence number
				return
			}
			height = res.Msg.Message.Height
		}
		return
	}
	// didn't decided -> purge messages
	//c.q.Purge(msgqueue.DefaultMsgIndex(message.SSVConsensusMsgType, c.Identifier)) // TODO: that's the right indexer? might need be height and all messages
	idn := hex.EncodeToString(c.Identifier)
	c.q.Clean(func(k string) bool {
		if strings.Contains(k, idn) && strings.Contains(k, fmt.Sprintf("%d", height)) {
			return true
		}
		return false
	})
}

// instanceStageChange processes a stage change for the current instance, returns true if requires stopping the instance after stage process.
func (c *Controller) instanceStageChange(stage qbft.RoundState) (bool, error) {
	c.logger.Debug("instance stage has been changed!", zap.String("stage", qbft.RoundStateName[int32(stage)]))
	switch stage {
	case qbft.RoundStatePrepare:
		if err := c.instanceStorage.SaveCurrentInstance(c.GetIdentifier(), c.getCurrentInstance().State()); err != nil {
			return true, errors.Wrap(err, "could not save prepare msg to storage")
		}
	case qbft.RoundStateDecided:
		run := func() error {
			agg, err := c.getCurrentInstance().CommittedAggregatedMsg()
			if err != nil {
				return errors.Wrap(err, "could not get aggregated commit msg and save to storage")
			}
			if err = c.decidedStrategy.SaveDecided(agg); err != nil {
				return errors.Wrap(err, "could not save highest decided message to storage")
			}

			ssvMsg, err := c.getCurrentInstance().GetCommittedAggSSVMessage()
			if err != nil {
				return errors.Wrap(err, "could not get SSV message aggregated commit msg")
			}
			c.logger.Debug("broadcasting decided message", zap.Any("msg", ssvMsg))
			if err = c.network.Broadcast(ssvMsg); err != nil {
				return errors.Wrap(err, "could not broadcast decided message")
			}
			c.logger.Info("decided current instance", zap.String("identifier", agg.Message.Identifier.String()),
				zap.Uint64("seqNum", uint64(agg.Message.Height)))
			return nil
		}

		err := run()
		// call stop after decided in order to prevent race condition
		c.getCurrentInstance().Stop()
		if err != nil {
			return true, err
		}
		return false, nil
	case qbft.RoundStateChangeRound:
		// set time for next round change
		c.getCurrentInstance().ResetRoundTimer()
		// broadcast round change
		if err := c.getCurrentInstance().BroadcastChangeRound(); err != nil {
			c.logger.Error("could not broadcast round change message", zap.Error(err))
		}

	case qbft.RoundStateStopped:
		c.logger.Info("current iBFT instance stopped, nilling currentInstance", zap.Uint64("seqNum", uint64(c.getCurrentInstance().State().GetHeight())))
		return true, nil
	}
	return false, nil
}

// fastChangeRoundCatchup fetches the latest change round (if one exists) from every peer to try and fast sync forward.
// This is an active msg fetching instead of waiting for an incoming msg to be received which can take a while
func (c *Controller) fastChangeRoundCatchup(instance instance.Instancer) {
	count := 0
	f := changeround.NewLastRoundFetcher(c.logger, c.network)
	handler := func(msg *message.SignedMessage) error {
		if c.getCurrentInstance() == nil {
			return errors.New("current instance is nil")
		}
		err := c.getCurrentInstance().ChangeRoundMsgValidationPipeline().Run(msg)
		if err != nil {
			return errors.Wrap(err, "invalid msg")
		}
		encodedMsg, err := msg.Encode()
		if err != nil {
			return errors.Wrap(err, "could not encode msg")
		}
		c.q.Add(&message.SSVMessage{
			MsgType: message.SSVConsensusMsgType, // should be consensus type as it change round msg
			ID:      c.Identifier,
			Data:    encodedMsg,
		})
		count++
		return nil
	}

	err := f.GetChangeRoundMessages(c.Identifier, instance.State().GetHeight(), handler)

	if err != nil {
		c.logger.Warn("failed fast change round catchup", zap.Error(err))
		return
	}

	c.logger.Info("fast change round catchup finished", zap.Int("count", count))
}
