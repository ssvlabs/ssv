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
instanceLoop:
	for {
		switch stage := <-stageChan; stage {
		case proto.RoundState_Prepare:
			if err := i.ibftStorage.SaveCurrentInstance(i.GetIdentifier(), i.currentInstance.State); err != nil {
				i.logger.Error("could not save prepare msg to storage", zap.Error(err))
			}
		case proto.RoundState_Decided:
			agg, err := i.currentInstance.CommittedAggregatedMsg()
			if err != nil {
				retRes = &InstanceResult{
					Decided: true,
					Error:   errors.WithMessage(err, "could not get aggregated commit msg and save to storage"),
				}
				break instanceLoop
			}
			if err := i.ibftStorage.SaveDecided(agg); err != nil {
				retRes = &InstanceResult{
					Decided: true,
					Error:   errors.WithMessage(err, "could not save aggregated commit msg to storage"),
				}
				break instanceLoop
			}
			if err := i.ibftStorage.SaveHighestDecidedInstance(agg); err != nil {
				retRes = &InstanceResult{
					Decided: true,
					Error:   errors.WithMessage(err, "could not save highest decided message to storage"),
				}
				break instanceLoop
			}
			if err := i.network.BroadcastDecided(i.ValidatorShare.PublicKey.Serialize(), agg); err != nil {
				retRes = &InstanceResult{
					Decided: true,
					Error:   errors.WithMessage(err, "could not broadcast decided message"),
				}
				break instanceLoop
			}
			i.logger.Info("decided current instance", zap.String("identifier", string(agg.Message.Lambda)), zap.Uint64("seqNum", agg.Message.SeqNumber))
			retRes = &InstanceResult{
				Decided: true,
				Msg:     agg,
				Error:   nil,
			}
		case proto.RoundState_Stopped:
			i.logger.Info("current iBFT instance stopped, nilling currentInstance", zap.Uint64("seqNum", i.currentInstance.State.SeqNumber))
			break instanceLoop
		}
	}
	i.currentInstance = nil
	return retRes, nil
}
