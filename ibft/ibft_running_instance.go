package ibft

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// startInstanceWithOptions will start an iBFT instance with the provided options.
// Does not pre-check instance validity and start validity!
func (i *ibftImpl) startInstanceWithOptions(instanceOpts InstanceOptions, value []byte) (chan *InstanceResult, error) {
	i.currentInstance = NewInstance(instanceOpts)
	i.currentInstance.Init()
	stageChan := i.currentInstance.GetStageChan()

	// reset leader seed for sequence
	if err := i.currentInstance.Start(value); err != nil {
		return nil, errors.WithMessage(err, "could not start iBFT instance")
	}

	i.CurrentInstanceResultChan = make(chan *InstanceResult)

	// main instance callback loop
	go func() {
		for {
			switch stage := <-stageChan; stage {
			case proto.RoundState_Prepare:
				if err := i.ibftStorage.SaveCurrentInstance(i.GetIdentifier(), i.currentInstance.State); err != nil {
					i.logger.Error("could not save prepare msg to storage", zap.Error(err))
				}
			case proto.RoundState_Decided:
				agg, err := i.currentInstance.CommittedAggregatedMsg()
				if err != nil {
					i.pushAndCloseInstanceResultChan(&InstanceResult{
						Decided: true,
						Error:   errors.WithMessage(err, "could not get aggregated commit msg and save to storage"),
					})
				}
				if err := i.ibftStorage.SaveDecided(agg); err != nil {
					i.pushAndCloseInstanceResultChan(&InstanceResult{
						Decided: true,
						Error:   errors.WithMessage(err, "could not save aggregated commit msg to storage"),
					})
				}
				if err := i.ibftStorage.SaveHighestDecidedInstance(agg); err != nil {
					i.pushAndCloseInstanceResultChan(&InstanceResult{
						Decided: true,
						Error:   errors.WithMessage(err, "could not save highest decided message to storage"),
					})
				}
				if err := i.network.BroadcastDecided(i.ValidatorShare.PublicKey.Serialize(), agg); err != nil {
					i.pushAndCloseInstanceResultChan(&InstanceResult{
						Decided: true,
						Error:   errors.WithMessage(err, "could not broadcast decided message"),
					})
				}
				i.logger.Info("decided current instance", zap.String("identifier", string(agg.Message.Lambda)), zap.Uint64("seqNum", agg.Message.SeqNumber))
				i.pushAndCloseInstanceResultChan(&InstanceResult{
					Decided: true,
					Msg:     agg,
					Error:   nil,
				})
			case proto.RoundState_Stopped:
				i.logger.Info("current iBFT instance stopped, nilling currentInstance")
				i.currentInstance = nil
				// Don't close result chan as other processes will handle it (like decided or sync)
				return
			}
		}
	}()
	return i.CurrentInstanceResultChan, nil
}

func (i *ibftImpl) pushAndCloseInstanceResultChan(res *InstanceResult) {
	i.instanceResultChanLock.Lock()
	defer i.instanceResultChanLock.Unlock()

	if i.CurrentInstanceResultChan != nil {
		i.CurrentInstanceResultChan <- res
	}

	// close
	if i.CurrentInstanceResultChan != nil {
		close(i.CurrentInstanceResultChan)
	}
	i.CurrentInstanceResultChan = nil
}
