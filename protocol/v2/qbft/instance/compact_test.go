package instance

import (
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
)

var compactTests = []struct {
	name     string
	input    *specqbft.State
	expected *specqbft.State
}{
	{
		name:     "empty",
		input:    &specqbft.State{},
		expected: &specqbft.State{},
	},
	{
		name: "empty but not nil",
		input: &specqbft.State{
			Round:                1,
			ProposeContainer:     &specqbft.MsgContainer{},
			PrepareContainer:     &specqbft.MsgContainer{},
			CommitContainer:      &specqbft.MsgContainer{},
			RoundChangeContainer: &specqbft.MsgContainer{},
		},
	},
	{
		name: "nothing to compact",
		input: &specqbft.State{
			Round: 1,
			ProposeContainer: &specqbft.MsgContainer{
				Msgs: map[specqbft.Round][]*specqbft.SignedMessage{
					1: {
						{Message: &specqbft.Message{Round: 1}},
					},
				},
			},
			PrepareContainer:     &specqbft.MsgContainer{},
			CommitContainer:      &specqbft.MsgContainer{},
			RoundChangeContainer: &specqbft.MsgContainer{},
		},
	},
}

func TestCompact(t *testing.T) {
}
