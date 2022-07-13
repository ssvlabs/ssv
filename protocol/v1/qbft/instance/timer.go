package instance

import (
	"context"
	"go.uber.org/zap"
)

func (i *Instance) startRoundTimerLoop() {
	ctx, cancel := context.WithCancel(i.ctx)
	defer cancel()
loop:
	for {
		if i.Stopped() {
			break loop
		}
		select {
		case <-ctx.Done():
			break loop
		case res := <-i.roundTimer.ResultChan():
			if res { // timed out
				i.uponChangeRoundTrigger()
			} else { // stopped
				i.Logger.Info("stopped timeout clock", zap.Uint64("round", uint64(i.State().GetRound())))
			}
		}

	}
	//i.roundTimer.CloseChan()
	i.Logger.Debug("instance round timer loop stopped")
}

// ResetRoundTimer ...
/**
"Timer:
	In addition to the state variables, each correct process pi also maintains a timer represented by timeri,
	which is used to trigger a round change when the algorithm does not sufficiently progress.
	The timer can be in one of two states: running or expired.
	When set to running, it is also set a time t(ri), which is an exponential function of the round number ri, after which the state changes to expired."

	ResetRoundTimer will reset the current timer (including stopping the previous one)
*/
func (i *Instance) ResetRoundTimer() {
	// stat new timer
	roundTimeout := i.roundTimeoutSeconds()
	i.roundTimer.Reset(roundTimeout)
	i.Logger.Info("started timeout clock", zap.Float64("seconds", roundTimeout.Seconds()), zap.Uint64("round", uint64(i.State().GetRound())))
}
