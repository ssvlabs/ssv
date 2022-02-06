package ibft

import (
	"github.com/bloxapp/ssv/ibft/instance/eventqueue"
	"github.com/bloxapp/ssv/network/msgqueue"
	"go.uber.org/zap"
	"sync"
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
loop:
	for {
		if i.Stopped() {
			break loop
		}

		if f := i.eventQueue.Pop(); f != nil {
			f()
		} else {
			time.Sleep(time.Millisecond * 100)
		}
	}
	i.Logger.Debug("instance main event loop stopped")
}

// StartMessagePipeline - the iBFT instance is message driven with an 'upon' logic.
// each message type has it's own pipeline of checks and actions, called by the networker implementation.
// Internal chan monitor if the instance reached decision or if a round change is required.
func (i *Instance) StartMessagePipeline() {
loop:
	for {
		if i.Stopped() {
			break loop
		}

		var wg sync.WaitGroup
		if queueCnt := i.MsgQueue.MsgCount(msgqueue.IBFTMessageIndexKey(i.State().Lambda.Get(), i.State().SeqNumber.Get())); queueCnt > 0 {
			logger := i.Logger.With(zap.Uint64("round", i.State().Round.Get()))
			logger.Debug("adding ibft message to event queue - waiting for done", zap.Int("queue msg count", queueCnt))
			wg.Add(1)
			if added := i.eventQueue.Add(eventqueue.NewEventWithCancel(func() {
				defer wg.Done()
				_, err := i.ProcessMessage()
				if err != nil {
					logger.Error("msg pipeline error", zap.Error(err))
				}
			}, wg.Done)); !added {
				logger.Debug("could not add ibft message to event queue")
				wg.Done()
				time.Sleep(time.Millisecond * 100)
			}
			// If we added a task to the queue, wait for it to finish and then loop again to add more
			wg.Wait()
		} else {
			time.Sleep(time.Millisecond * 100)
		}
	}
	i.Logger.Debug("instance msg pipeline loop stopped")
}

func (i *Instance) startRoundTimerLoop() {
loop:
	for {
		if i.Stopped() {
			break loop
		}
		res := <-i.roundTimer.ResultChan()
		if res { // timed out
			i.eventQueue.Add(eventqueue.NewEvent(func() {
				i.uponChangeRoundTrigger()
			}))
		} else { // stopped
			i.Logger.Info("stopped timeout clock", zap.Uint64("round", i.State().Round.Get()))
		}
	}
	//i.roundTimer.CloseChan()
	i.Logger.Debug("instance round timer loop stopped")
}

/**
"Timer:
	In addition to the state variables, each correct process pi also maintains a timer represented by timeri,
	which is used to trigger a round change when the algorithm does not sufficiently progress.
	The timer can be in one of two states: running or expired.
	When set to running, it is also set a time t(ri), which is an exponential function of the round number ri, after which the state changes to expired."

	resetRoundTimer will reset the current timer (including stopping the previous one)
*/
func (i *Instance) resetRoundTimer() {
	// stat new timer
	roundTimeout := i.roundTimeoutSeconds()
	i.roundTimer.Reset(roundTimeout)
	i.Logger.Info("started timeout clock", zap.Float64("seconds", roundTimeout.Seconds()), zap.Uint64("round", i.State().Round.Get()))
}
