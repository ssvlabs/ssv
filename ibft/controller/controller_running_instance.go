package controller

import (
	"context"
	"github.com/bloxapp/ssv/ibft"
	instance "github.com/bloxapp/ssv/ibft/instance"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/sync/speedup"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/bloxapp/ssv/utils/format"
	"github.com/bloxapp/ssv/utils/tasks"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"time"
)

// startInstanceWithOptions will start an iBFT instance with the provided options.
// Does not pre-check instance validity and start validity!
func (i *Controller) startInstanceWithOptions(instanceOpts *instance.InstanceOptions, value []byte) (*ibft.InstanceResult, error) {
	i.currentInstance = instance.NewInstance(instanceOpts)
	i.currentInstance.Init()
	stageChan := i.currentInstance.GetStageChan()

	// reset leader seed for sequence
	if err := i.currentInstance.Start(value); err != nil {
		return nil, errors.WithMessage(err, "could not start iBFT instance")
	}

	pk, role := format.IdentifierUnformat(string(i.Identifier))
	metricsCurrentSequence.WithLabelValues(role, pk).Set(float64(i.currentInstance.State().SeqNumber.Get()))

	// catch up if we can
	go i.fastChangeRoundCatchup(i.currentInstance)

	// main instance callback loop
	var retRes *ibft.InstanceResult
	var err error
instanceLoop:
	for {
		stage := <-stageChan
		if i.currentInstance == nil {
			i.logger.Debug("stage channel was invoked but instance is already empty", zap.Any("stage", stage))
			break instanceLoop
		}
		exit, e := i.instanceStageChange(stage)
		if e != nil {
			err = e
			break instanceLoop
		}
		if exit {
			// exited with no error means instance decided
			// fetch decided msg and return
			retMsg, found, e := i.ibftStorage.GetDecided(i.Identifier, instanceOpts.SeqNumber)
			if !found {
				err = errors.New("could not find decided msg after instance finished")
				break instanceLoop
			}
			if e != nil {
				err = e
				break instanceLoop
			}
			if retMsg == nil {
				err = errors.New("could not fetch decided msg after instance finished")
				break instanceLoop
			}
			retRes = &ibft.InstanceResult{
				Decided: true,
				Msg:     retMsg,
			}
			break instanceLoop
		}
	}
	var seq uint64
	if i.currentInstance != nil {
		// saves seq as instance will be cleared
		seq = i.currentInstance.State().SeqNumber.Get()
		// when main instance loop breaks, nil current instance
		i.currentInstance = nil
	}
	i.logger.Debug("iBFT instance result loop stopped")

	i.afterInstance(seq, retRes, err)

	return retRes, err
}

// afterInstance is triggered after the instance was finished
func (i *Controller) afterInstance(seq uint64, res *ibft.InstanceResult, err error) {
	// if instance was decided -> wait for late commit messages
	decided := res != nil && res.Decided
	if decided && err == nil {
		if seq == uint64(0) {
			if res.Msg == nil || res.Msg.Message == nil {
				// missing sequence number
				return
			}
			seq = res.Msg.Message.SeqNumber
		}
		go i.listenToLateCommitMsgs(i.Identifier[:], seq)
		return
	}
	// didn't decided -> purge messages
	i.msgQueue.PurgeIndexedMessages(msgqueue.IBFTMessageIndexKey(i.Identifier[:], seq))
}

// instanceStageChange processes a stage change for the current instance, returns true if requires stopping the instance after stage process.
func (i *Controller) instanceStageChange(stage proto.RoundState) (bool, error) {
	switch stage {
	case proto.RoundState_Prepare:
		if err := i.ibftStorage.SaveCurrentInstance(i.GetIdentifier(), i.currentInstance.State()); err != nil {
			return true, errors.Wrap(err, "could not save prepare msg to storage")
		}
	case proto.RoundState_Decided:
		agg, err := i.currentInstance.CommittedAggregatedMsg()
		if err != nil {
			return true, errors.Wrap(err, "could not get aggregated commit msg and save to storage")
		}
		if err = i.ibftStorage.SaveDecided(agg); err != nil {
			return true, errors.Wrap(err, "could not save aggregated commit msg to storage")
		}
		if err = i.ibftStorage.SaveHighestDecidedInstance(agg); err != nil {
			return true, errors.Wrap(err, "could not save highest decided message to storage")
		}
		if err = i.network.BroadcastDecided(i.ValidatorShare.PublicKey.Serialize(), agg); err != nil {
			return true, errors.Wrap(err, "could not broadcast decided message")
		}
		i.logger.Info("decided current instance", zap.String("identifier", string(agg.Message.Lambda)), zap.Uint64("seqNum", agg.Message.SeqNumber))
		return false, nil
	case proto.RoundState_Stopped:
		i.logger.Info("current iBFT instance stopped, nilling currentInstance", zap.Uint64("seqNum", i.currentInstance.State().SeqNumber.Get()))
		return true, nil
	}
	return false, nil
}

// listenToLateCommitMsgs handles late arrivals of commit messages and pick up orphan commit messages that
// were not included in the decided message when quorum was achieved
func (i *Controller) listenToLateCommitMsgs(identifier []byte, seq uint64) {
	idxKey := msgqueue.IBFTMessageIndexKey(identifier, seq)
	defer i.msgQueue.PurgeIndexedMessages(idxKey)

	f := func(stopper tasks.Stopper) (interface{}, error) {
	loop:
		for {
			if stopper.IsStopped() {
				break loop
			}
			if netMsg := i.msgQueue.PopMessage(idxKey); netMsg != nil && netMsg.SignedMessage != nil {
				if netMsg.SignedMessage.Message.Type != proto.RoundState_Commit {
					// not a commit message -> skip
					continue
				}
				logger := i.logger.With(zap.Uint64("seq", netMsg.SignedMessage.Message.SeqNumber),
					zap.Uint64s("signers", netMsg.SignedMessage.SignerIds))
				// TODO: need to use the fork
				if err := instance.CommitMsgValidationPipelineV0(identifier, seq, i.ValidatorShare).Run(netMsg.SignedMessage); err != nil {
					i.logger.Error("received invalid late commit message", zap.Error(err))
					continue
				}
				updated, err := instance.ProcessLateCommitMsg(netMsg.SignedMessage, i.ibftStorage,
					i.ValidatorShare)
				if err != nil {
					logger.Error("failed to process late commit message", zap.Error(err))
				} else if updated != nil {
					logger.Debug("decided message was updated", zap.Uint64s("updated signers", updated.SignerIds))
					if err := i.network.BroadcastDecided(i.ValidatorShare.PublicKey.Serialize(), updated); err != nil {
						logger.Error("could not broadcast decided message", zap.Error(err))
					}
					logger.Debug("updated decided was broadcasted")
					ibft.ReportDecided(i.ValidatorShare.PublicKey.SerializeToHexStr(), updated)
					break loop
				}
			} else {
				time.Sleep(time.Millisecond * 100)
			}
		}
		return nil, nil
	}

	i.logger.Debug("started listening to late commit msgs", zap.Uint64("seq_number", seq))
	_, _, _ = tasks.ExecWithTimeout(context.Background(), f, time.Minute*6)
	i.logger.Debug("stopped listening to late commit msgs", zap.Uint64("seq_number", seq))
}

// fastChangeRoundCatchup fetches the latest change round (if one exists) from every peer to try and fast sync forward.
// This is an active msg fetching instead of waiting for an incoming msg to be received which can take a while
func (i *Controller) fastChangeRoundCatchup(instance ibft.Instance) {
	sync := speedup.New(
		i.logger,
		i.Identifier,
		i.ValidatorShare.PublicKey.Serialize(),
		instance.State().SeqNumber.Get(),
		i.network,
		instance.ChangeRoundMsgValidationPipeline(),
	)
	msgs, err := sync.Start()
	if err != nil {
		i.logger.Error("failed fast change round catchup", zap.Error(err))
	} else {
		for _, msg := range msgs {
			i.msgQueue.AddMessage(&network.Message{
				SignedMessage: msg,
				Type:          network.NetworkMsg_IBFTType,
			})
		}
		i.logger.Info("fast change round catchup finished", zap.Int("found_msgs", len(msgs)))
	}
}
