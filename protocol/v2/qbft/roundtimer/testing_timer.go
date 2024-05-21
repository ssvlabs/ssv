package roundtimer

import specqbft "github.com/ssvlabs/ssv-spec/qbft"

type TimerState struct {
	Timeouts int
	Round    specqbft.Round
}

type TestQBFTTimer struct {
	State TimerState
}

func NewTestingTimer() specqbft.Timer {
	return &TestQBFTTimer{
		State: TimerState{},
	}
}

func (t *TestQBFTTimer) TimeoutForRound(round specqbft.Round) {
	t.State.Timeouts++
	t.State.Round = round
}
