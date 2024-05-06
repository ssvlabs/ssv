package roundtimer

import genesisspecqbft "github.com/bloxapp/ssv-spec/qbft"

type TimerState struct {
	Timeouts int
	Round    genesisspecqbft.Round
}

type TestQBFTTimer struct {
	State TimerState
}

func NewTestingTimer() Timer {
	return &TestQBFTTimer{
		State: TimerState{},
	}
}

func (t *TestQBFTTimer) TimeoutForRound(height genesisspecqbft.Height, round genesisspecqbft.Round) {
	t.State.Timeouts++
	t.State.Round = round
}
