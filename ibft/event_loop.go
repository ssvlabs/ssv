package ibft

import (
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
	i.Logger.Info("instance main event loop stopped")
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
		if queueCnt := i.MsgQueue.MsgCount(msgqueue.IBFTMessageIndexKey(i.State.Lambda.Get(), i.State.SeqNumber.Get(), i.State.Round.Get())); queueCnt > 0 {
			i.Logger.Debug("adding ibft message to event queue - waiting for done", zap.Int("queue msg count", queueCnt))
			wg.Add(1)
			if added := i.eventQueue.Add(func() {
				_, err := i.ProcessMessage()
				if err != nil {
					i.Logger.Error("msg pipeline error", zap.Error(err))
				}
				wg.Done()
			}); !added {
				i.Logger.Debug("could not add ibft message to event queue")
				time.Sleep(time.Millisecond * 100)
				wg.Done()
			}
			// If we added a task to the queue, wait for it to finish and then loop again to add more
			wg.Wait()
		} else {
			time.Sleep(time.Millisecond * 100)
		}
	}
	i.Logger.Info("instance msg pipeline loop stopped")
}

// StartPartialChangeRoundPipeline continuously tries to find partial change round quorum
func (i *Instance) StartPartialChangeRoundPipeline() {
loop:
	for {
		if i.Stopped() {
			break loop
		}

		var wg sync.WaitGroup
		if i.MsgQueue.MsgCount(msgqueue.IBFTAllRoundChangeIndexKey(i.State.Lambda.Get(), i.State.SeqNumber.Get())) > 0 {
			wg.Add(1)
			if added := i.eventQueue.Add(func() {
				found, err := i.ProcessChangeRoundPartialQuorum()
				if err != nil {
					i.Logger.Error("failed finding partial change round quorum", zap.Error(err))
				}
				if !found {
					// if not found, wait 1 second and then finish to try again
					time.Sleep(time.Second * 1)
				}
				//i.Logger.Debug("done with round change message")
				wg.Done()
			}); !added {
				i.Logger.Debug("could not add round change to event queue")
				time.Sleep(time.Second * 1)
				wg.Done()
			}
			// If we added a task to the queue, wait for it to finish and then loop again to add more
			wg.Wait()
		} else {
			time.Sleep(time.Second * 1)
		}
	}
	i.Logger.Info("instance partial change round pipeline loop stopped")
}

func (i *Instance) startRoundTimerLoop() {
loop:
	for {
		if i.Stopped() {
			break loop
		}

		res := <-i.roundTimer.ResultChan()
		if res { // timed out
			i.eventQueue.Add(func() {
				i.uponChangeRoundTrigger()
			})
		} else { // stopped
			i.Logger.Info("stopped timeout clock", zap.Uint64("round", i.Round()))
		}
	}
	i.Logger.Info("instance round timer loop stopped")
}

/**
"Timer:
	In addition to the State variables, each correct process pi also maintains a timer represented by timeri,
	which is used to trigger a round change when the algorithm does not sufficiently progress.
	The timer can be in one of two states: running or expired.
	When set to running, it is also set a time t(ri), which is an exponential function of the round number ri, after which the State changes to expired."

	resetRoundTimer will reset the current timer (including stopping the previous one)
*/
func (i *Instance) resetRoundTimer() {
	// stat new timer
	roundTimeout := i.roundTimeoutSeconds()
	i.roundTimer.Reset(roundTimeout)
	i.Logger.Info("started timeout clock", zap.Float64("seconds", roundTimeout.Seconds()), zap.Uint64("round", i.Round()))
}

func (i *Instance) stopRoundTimer() {
	i.Logger.Info("stopping timeout clock")
	i.roundTimer.Stop()
}
