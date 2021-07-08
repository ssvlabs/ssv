package ibft

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// startInstanceWithOptions will start an iBFT instance with the provided options.
// Does not pre-check instance validity and start validity!
func (i *ibftImpl) startInstanceWithOptions(instanceOpts InstanceOptions, value []byte) (*InstanceResult, error) {
	i.currentInstance = NewInstance(instanceOpts)
	i.currentInstance.Init()
	stageChan := i.currentInstance.GetStageChan()

	// reset leader seed for sequence
	if err := i.currentInstance.Start(value); err != nil {
		return nil, errors.WithMessage(err, "could not start iBFT instance")
	}

	// main instance callback loop
	var retRes *InstanceResult
	var err error
instanceLoop:
	for {
		select {
		case stage := <-stageChan:
			e, exit := i.instanceStageChange(stage)
			if e != nil {
				err = e
				break instanceLoop
			}
			if exit { // exited with no error means instance decided
				// fetch decided msg and return
				retMsg, e := i.ibftStorage.GetDecided(i.Identifier, instanceOpts.SeqNumber)
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
	}
	// when main instance loop breaks, nil current instance
	i.currentInstance = nil
	return retRes, err
}

// instanceStageChange processes a stage change for the current instance, returns true if requires stopping the instance after stage process.
func (i *ibftImpl) instanceStageChange(stage proto.RoundState) (error, bool) {
	switch stage {
	case proto.RoundState_Prepare:
		if err := i.ibftStorage.SaveCurrentInstance(i.GetIdentifier(), i.currentInstance.State); err != nil {
			return errors.Wrap(err, "could not save prepare msg to storage"), true
		}
	case proto.RoundState_Decided:
		agg, err := i.currentInstance.CommittedAggregatedMsg()
		if err != nil {
			return errors.Wrap(err, "could not get aggregated commit msg and save to storage"), true
		}
		if err := i.ibftStorage.SaveDecided(agg); err != nil {
			return errors.Wrap(err, "could not save aggregated commit msg to storage"), true
		}
		if err := i.ibftStorage.SaveHighestDecidedInstance(agg); err != nil {
			return errors.Wrap(err, "could not save highest decided message to storage"), true
		}
		if err := i.network.BroadcastDecided(i.ValidatorShare.PublicKey.Serialize(), agg); err != nil {
			return errors.Wrap(err, "could not broadcast decided message"), true
		}
		i.logger.Info("decided current instance", zap.String("identifier", string(agg.Message.Lambda)), zap.Uint64("seqNum", agg.Message.SeqNumber))
		return nil, false
	case proto.RoundState_Stopped:
		i.logger.Info("current iBFT instance stopped, nilling currentInstance", zap.Uint64("seqNum", i.currentInstance.State.SeqNumber))
		return nil, true
	}
	return nil, false
}
