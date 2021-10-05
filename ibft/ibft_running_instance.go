package ibft

import (
	"context"
	"time"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/sync/speedup"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/utils/format"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/bloxapp/ssv/utils/tasks"
)

// startInstanceWithOptions will start an iBFT instance with the provided options.
// Does not pre-check instance validity and start validity!
func (i *ibftImpl) startInstanceWithOptions(instanceOpts *InstanceOptions, value []byte) (*InstanceResult, error) {
	i.currentInstance = NewInstance(instanceOpts)
	i.currentInstance.Init()
	stageChan := i.currentInstance.GetStageChan()

	// reset leader seed for sequence
	if err := i.currentInstance.Start(value); err != nil {
		return nil, errors.WithMessage(err, "could not start iBFT instance")
	}

	pk, role := format.IdentifierUnformat(string(i.Identifier))
	metricsCurrentSequence.WithLabelValues(role, pk).Set(float64(i.currentInstance.State.SeqNumber.Get()))

	// catch up if we can
	go i.fastChangeRoundCatchup(i.currentInstance)

	// main instance callback loop
	var retRes *InstanceResult
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
		if exit { // exited with no error means instance decided
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
			retRes = &InstanceResult{
				Decided: true,
				Msg:     retMsg,
			}
			break instanceLoop
		}
	}
	// when main instance loop breaks, nil current instance
	i.currentInstance = nil
	i.logger.Debug("iBFT instance result loop stopped")
	return retRes, err
}

// instanceStageChange processes a stage change for the current instance, returns true if requires stopping the instance after stage process.
func (i *ibftImpl) instanceStageChange(stage proto.RoundState) (bool, error) {
	switch stage {
	case proto.RoundState_Prepare:
		if err := i.ibftStorage.SaveCurrentInstance(i.GetIdentifier(), i.currentInstance.State); err != nil {
			return true, errors.Wrap(err, "could not save prepare msg to storage")
		}
	case proto.RoundState_Decided:
		agg, err := i.currentInstance.CommittedAggregatedMsg()
		if err != nil {
			return true, errors.Wrap(err, "could not get aggregated commit msg and save to storage")
		}
		if err := i.ibftStorage.SaveDecided(agg); err != nil {
			return true, errors.Wrap(err, "could not save aggregated commit msg to storage")
		}
		if err := i.ibftStorage.SaveHighestDecidedInstance(agg); err != nil {
			return true, errors.Wrap(err, "could not save highest decided message to storage")
		}
		if err := i.network.BroadcastDecided(i.ValidatorShare.PublicKey.Serialize(), agg); err != nil {
			return true, errors.Wrap(err, "could not broadcast decided message")
		}
		i.logger.Info("decided current instance", zap.String("identifier", string(agg.Message.Lambda)), zap.Uint64("seqNum", agg.Message.SeqNumber))
		go i.listenToLateCommitMsgs(i.currentInstance)
		return false, nil
	case proto.RoundState_Stopped:
		i.logger.Info("current iBFT instance stopped, nilling currentInstance", zap.Uint64("seqNum", i.currentInstance.State.SeqNumber.Get()))
		return true, nil
	}
	return false, nil
}

// listenToLateCommitMsgs handles late arrivals of commit messages as the ibft instance terminates after a quorum
// is reached which doesn't guarantee that late commit msgs will be aggregated into the stored decided msg.
func (i *ibftImpl) listenToLateCommitMsgs(runningInstance *Instance) {
	f := func(stopper tasks.Stopper) (interface{}, error) {
	loop:
		for {
			if stopper.IsStopped() {
				break loop
			}

			if netMsg := runningInstance.MsgQueue.PopMessage(msgqueue.IBFTMessageIndexKey(runningInstance.State.Lambda.Get(), runningInstance.State.SeqNumber.Get(), runningInstance.State.Round.Get())); netMsg != nil {
				if netMsg.SignedMessage.Message.Type == proto.RoundState_Commit {
					// verify message
					if err := runningInstance.commitMsgValidationPipeline().Run(netMsg.SignedMessage); err != nil {
						i.logger.Error("received invalid late commit message", zap.Error(err))
						continue
					}

					// find stored decided
					decidedMsg, found, err := i.ibftStorage.GetDecided(netMsg.SignedMessage.Message.Lambda, netMsg.SignedMessage.Message.SeqNumber)
					if err != nil {
						i.logger.Error("could not fetch decided for late commit message", zap.Error(err))
						continue
					}
					if !found {
						i.logger.Error("received late commit message for unknown decided", zap.Error(err))
						continue
					}

					// aggregate message with stored decided
					if err := decidedMsg.Aggregate(netMsg.SignedMessage); err != nil {
						if err == proto.ErrDuplicateMsgSigner {
							i.logger.Debug("received late commit message with duplicate signers, not saving to storage")
							continue
						}
						i.logger.Error("could not aggregate late commit message with known decided msg", zap.Error(err))
						continue
					}

					// save to storage
					if err := i.ibftStorage.SaveDecided(decidedMsg); err != nil {
						i.logger.Error("could not save aggregated late commit message", zap.Error(err))
					}
				}
			} else {
				time.Sleep(time.Millisecond * 100)
			}
		}

		return nil, nil
	}

	i.logger.Debug("started listening to late commit msgs", zap.Uint64("seq_number", runningInstance.State.SeqNumber.Get()))
	tasks.ExecWithTimeout(context.Background(), f, time.Minute*6)
	i.logger.Debug("stopped listening to late commit msgs")
}

// fastChangeRoundCatchup fetches the latest change round (if one exists) from every peer to try and fast sync forward.
// This is an active msg fetching instead of waiting for an incoming msg to be received which can take a while
func (i *ibftImpl) fastChangeRoundCatchup(instance *Instance) {
	sync := speedup.New(
		i.logger,
		i.Identifier,
		i.ValidatorShare.PublicKey.Serialize(),
		instance.State.SeqNumber.Get(),
		i.network,
		instance.changeRoundMsgValidationPipeline(),
	)
	msgs, err := sync.Start()
	if err != nil {
		i.logger.Error("failed fast change round catchup", zap.Error(err))
	} else {
		for _, msg := range msgs {
			instance.MsgQueue.AddMessage(&network.Message{
				SignedMessage: msg,
				Type:          network.NetworkMsg_IBFTType,
			})
		}
		i.logger.Info("fast change round catchup finished", zap.Int("found_msgs", len(msgs)))
	}
}
