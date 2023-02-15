package instance

import specqbft "github.com/bloxapp/ssv-spec/qbft"

func Compact(state *specqbft.State) {
	wholeCommitteeDecided := len(state.CommitContainer.Msgs) == len(state.Share.Committee)

	compactContainer(state.ProposeContainer, state.Decided)
	compactContainer(state.PrepareContainer, state.Decided)
	compactContainer(state.RoundChangeContainer, state.Decided)

	// Only discard commit messages if the whole committee decided,
	// otherwise just discard messages from previous rounds.
	compactContainer(state.CommitContainer, wholeCommitteeDecided)
}

func compactContainer(container *specqbft.MsgContainer, discard bool) {
	switch {
	case container == nil || len(container.Msgs) == 0:
		// Empty already.
	case discard:
		// Discard all messages.
		container.Msgs = map[specqbft.Round][]*specqbft.SignedMessage{}
	default:
		// Discard messages from previous rounds.
		var highestRound specqbft.Round
		for round := range container.Msgs {
			if round > highestRound {
				highestRound = round
			}
		}
		container.Msgs = map[specqbft.Round][]*specqbft.SignedMessage{
			highestRound: container.Msgs[highestRound],
		}
	}
}
