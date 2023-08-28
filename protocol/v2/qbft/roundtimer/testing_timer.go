package roundtimer

import specqbft "github.com/bloxapp/ssv-spec/qbft"

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
