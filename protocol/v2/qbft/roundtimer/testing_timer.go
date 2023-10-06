package roundtimer

import (
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
)

type TimerState struct {
	Timeouts int
	Round    specqbft.Round
}

type TestQBFTTimer struct {
	State TimerState
}

func NewTestingTimer() Timer {
	return &TestQBFTTimer{
		State: TimerState{},
	}
}

func (t *TestQBFTTimer) TimeoutForRound(height specqbft.Height, round specqbft.Round) {
	t.State.Timeouts++
	t.State.Round = round
}

func (t *TestQBFTTimer) GetChannel() <-chan time.Time {
	// A dummy channel that won't block or send anything
	ch := make(chan time.Time)
	close(ch)
	return ch
}

func (t *TestQBFTTimer) GetRole() spectypes.BeaconRole {
	return spectypes.BeaconRole(0)
}

func (t *TestQBFTTimer) Round() specqbft.Round {
	return t.State.Round
}

func (t *TestQBFTTimer) IsActive() bool {
	return true
}
