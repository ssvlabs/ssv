package instance

import specqbft "github.com/bloxapp/ssv-spec/qbft"

// Compact trims the given qbft.State down to the minimum required
// for consensus to proceed.
//
// Compact always discards message from previous rounds.
// Compact discards non-commit messages from the current round only if it's decided.
// Compact discards commit messages from the current round only if the whole committee decided.
//
// This helps reduce the state's memory footprint.
func Compact(state *specqbft.State) {
	compactContainer(state.ProposeContainer, state.Decided)
	compactContainer(state.PrepareContainer, state.Decided)
	compactContainer(state.RoundChangeContainer, state.Decided)

	// Only discard commit messages if the whole committee decided,
	// otherwise just trim down to the highest round.
	wholeCommitteeDecided := len(state.CommitContainer.Msgs) == len(state.Share.Committee)
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
		// Trim down to only the highest round.
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
