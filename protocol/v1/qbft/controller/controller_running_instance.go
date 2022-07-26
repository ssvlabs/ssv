package controller

import (
	"encoding/hex"

	spectypes "github.com/bloxapp/ssv-spec/types"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/qbft/msgqueue"

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

	messageID := message.ToMessageID(c.Identifier)
	metricsCurrentSequence.WithLabelValues(messageID.GetRoleType().String(), hex.EncodeToString(messageID.GetPubKey())).Set(float64(newInstance.State().GetHeight()))

	// catch up if we can
	go c.fastChangeRoundCatchup(newInstance)

	// main instance callback loop
	var retRes *instance.Result
	var err error
instanceLoop:
	for {
		stage := <-stageChan
		if c.GetCurrentInstance() == nil {
			c.Logger.Debug("stage channel was invoked but instance is already empty", zap.Any("stage", stage))
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
			retMsg, e := c.DecidedStrategy.GetDecided(c.Identifier, instanceOpts.Height, instanceOpts.Height)
			if e != nil {
				err = e
				c.Logger.Error("failed to get decided when instance exist", zap.Error(e))
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
	var seq specqbft.Height
	if c.GetCurrentInstance() != nil {
		// saves seq as instance will be cleared
		seq = c.GetCurrentInstance().State().GetHeight()
		// when main instance loop breaks, nil current instance
		c.setCurrentInstance(nil)
	}
	c.Logger.Debug("iBFT instance result loop stopped")

	c.afterInstance(seq, retRes, err)

	return retRes, err
}

// afterInstance is triggered after the instance was finished
func (c *Controller) afterInstance(height specqbft.Height, res *instance.Result, err error) {
	// if instance was decided -> wait for late commit messages
	decided := res != nil && res.Decided
	if decided && err == nil {
		if height == specqbft.Height(0) {
			if res.Msg == nil || res.Msg.Message == nil {
				// missing sequence number
				return
			}
			height = res.Msg.Message.Height
		}
		return
	}
	// didn't decided -> purge messages with smaller height
	//c.q.Purge(msgqueue.DefaultMsgIndex(message.SSVConsensusMsgType, c.Identifier)) // TODO: that's the right indexer? might need be height and all messages
	idn := hex.EncodeToString(c.Identifier)
	c.Q.Clean(func(k msgqueue.Index) bool {
		if k.ID == idn && k.H <= height {
			if k.Cmt == specqbft.CommitMsgType && k.H == height {
				return false
			}
			return true
		}
		return false
	})
}

// instanceStageChange processes a stage change for the current instance, returns true if requires stopping the instance after stage process.
func (c *Controller) instanceStageChange(stage qbft.RoundState) (bool, error) {
	logger := c.Logger.With()
	if ci := c.GetCurrentInstance(); ci != nil {
		if s := ci.State(); s != nil {
			logger = logger.With(zap.Uint64("instanceHeight", uint64(s.GetHeight())))
		}
	}
	logger.Debug("instance stage has been changed!", zap.String("stage", qbft.RoundStateName[int32(stage)]))
	switch stage {
	case qbft.RoundStatePrepare:
		if err := c.InstanceStorage.SaveCurrentInstance(c.GetIdentifier(), c.GetCurrentInstance().State()); err != nil {
			return true, errors.Wrap(err, "could not save prepare msg to storage")
		}
	case qbft.RoundStateDecided:
		run := func() error {
			agg, err := c.GetCurrentInstance().CommittedAggregatedMsg()
			if err != nil {
				return errors.Wrap(err, "could not get aggregated commit msg and save to storage")
			}
			updated, err := c.DecidedStrategy.UpdateDecided(agg)
			if err != nil {
				return errors.Wrap(err, "could not save highest decided message to storage")
			}
			logger.Info("decided current instance",
				zap.String("identifier", message.ToMessageID(agg.Message.Identifier).String()),
				zap.Any("signers", agg.GetSigners()),
				zap.Uint64("height", uint64(agg.Message.Height)),
				zap.Any("updated", updated))
			if updated != nil {
				if err = c.onNewDecidedMessage(updated); err != nil {
					return err
				}
			}
			return nil
		}

		err := run()
		// call stop after decided in order to prevent race condition
		c.GetCurrentInstance().Stop()
		if err != nil {
			return true, err
		}
		return false, nil
	case qbft.RoundStateChangeRound:
		// set time for next round change
		c.GetCurrentInstance().ResetRoundTimer()
		// broadcast round change
		if err := c.GetCurrentInstance().BroadcastChangeRound(); err != nil {
			c.Logger.Error("could not broadcast round change message", zap.Error(err))
		}
	case qbft.RoundStateStopped:
		c.Logger.Info("current iBFT instance stopped, nilling currentInstance")
		return true, nil
	}
	return false, nil
}

// fastChangeRoundCatchup fetches the latest change round (if one exists) from every peer to try and fast sync forward.
// This is an active msg fetching instead of waiting for an incoming msg to be received which can take a while
func (c *Controller) fastChangeRoundCatchup(instance instance.Instancer) {
	count := 0
	f := changeround.NewLastRoundFetcher(c.Logger, c.Network)
	handler := func(msg *specqbft.SignedMessage) error {
		if ctxErr := c.Ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		currentInstance := c.GetCurrentInstance()
		if currentInstance == nil {
			return errors.New("current instance is nil")
		}
		logger := c.Logger.With(zap.Uint64("msgHeight", uint64(msg.Message.Height)))
		if stage := currentInstance.State(); stage != nil && stage.GetHeight() > msg.Message.Height {
			logger.Debug("change round message is old, ignoring",
				zap.Uint64("currentHeight", uint64(stage.GetHeight())))
			return nil
		} else if stage.GetHeight() < msg.Message.Height {
			logger.Debug("got change round message of an newer decided",
				zap.Uint64("currentHeight", uint64(stage.GetHeight())))
		}
		err := c.GetCurrentInstance().ChangeRoundMsgValidationPipeline().Run(msg)
		if err != nil {
			return errors.Wrap(err, "invalid msg")
		}
		encodedMsg, err := msg.Encode()
		if err != nil {
			return errors.Wrap(err, "could not encode msg")
		}
		c.Q.Add(&spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType, // should be consensus type as it change round msg
			MsgID:   message.ToMessageID(c.Identifier),
			Data:    encodedMsg,
		})
		count++
		return nil
	}

	h := instance.State().GetHeight()
	err := f.GetChangeRoundMessages(message.ToMessageID(c.Identifier), h, handler)

	if err != nil {
		c.Logger.Warn("failed fast change round catchup", zap.Error(err))
		return
	}

	c.Logger.Info("fast change round catchup finished", zap.Int("count", count), zap.Int64("height", int64(h)))
}
