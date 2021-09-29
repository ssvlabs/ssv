package ibft

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/sync/speedup"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/utils/format"
	"github.com/pkg/errors"
	"go.uber.org/zap"
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
			if !found{
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
		return false, nil
	case proto.RoundState_Stopped:
		i.logger.Info("current iBFT instance stopped, nilling currentInstance", zap.Uint64("seqNum", i.currentInstance.State.SeqNumber.Get()))
		return true, nil
	}
	return false, nil
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
