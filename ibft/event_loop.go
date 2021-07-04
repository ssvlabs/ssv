package ibft

import (
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/prysmaticlabs/prysm/shared/mathutil"
	"go.uber.org/zap"
	"time"
)

/**
Multi threading in iBFT instance -

The iBFT instance is a single thread service.
All the different events (reading network messages, timeouts, etc) are brokered through an event queue.

Events are added asynchronically to the queue but are pulled synchronically in just one place, the main event loop.



***** async ******\\\\\\\\\\\\\\\\\\  sync  \\\\\\\\\\\\\\\\\\\\\\\
Network message->|
Timeouts->		 |-> added to event queue -> pulled from event queue
Other events->   |


*/

// StartMainEventLoop start the main event loop queue for the iBFT instance which iterates events in the queue, if non found it will wait before trying again.
func (i *Instance) StartMainEventLoop() {
	for {
		if i.Stopped() {
			return
		}

		if f := i.eventQueue.Pop(); f != nil {
			f()
		} else {
			time.Sleep(time.Millisecond * 100)
		}
	}
}

// StartMessagePipeline - the iBFT instance is message driven with an 'upon' logic.
// each message type has it's own pipeline of checks and actions, called by the networker implementation.
// Internal chan monitor if the instance reached decision or if a round change is required.
func (i *Instance) StartMessagePipeline() {
	for {
		if i.Stopped() {
			return
		}

		// TODO - refactor
		if i.MsgQueue.MsgCount(msgqueue.IBFTRoundIndexKey(i.State.Lambda, i.State.SeqNumber, i.State.Round)) > 0 {
			i.eventQueue.Add(func() {
				_, err := i.ProcessMessage()
				if err != nil {
					i.Logger.Error("msg pipeline error", zap.Error(err))
				}
			})
		} else {
			time.Sleep(time.Millisecond * 100)
		}
	}
}

/**
"Timer:
	In addition to the State variables, each correct process pi also maintains a timer represented by timeri,
	which is used to trigger a round change when the algorithm does not sufficiently progress.
	The timer can be in one of two states: running or expired.
	When set to running, it is also set a time t(ri), which is an exponential function of the round number ri, after which the State changes to expired."
*/
func (i *Instance) triggerRoundChangeOnTimer() {
	go func() {
		// make sure previous timer is stopped
		i.stopRoundChangeTimer()

		// stat new timer
		roundTimeout := uint64(i.Config.RoundChangeDuration) * mathutil.PowerOf2(i.State.Round)
		i.roundChangeTimer = time.NewTimer(time.Duration(roundTimeout))
		i.Logger.Info("started timeout clock", zap.Float64("seconds", time.Duration(roundTimeout).Seconds()))

		<-i.roundChangeTimer.C
		i.eventQueue.Add(func() {
			i.stopRoundChangeTimer()
			i.uponChangeRoundTrigger()
		})
	}()
}

func (i *Instance) stopRoundChangeTimer() {
	if i.roundChangeTimer != nil {
		i.roundChangeTimer.Stop()
		i.roundChangeTimer = nil
	}
}
