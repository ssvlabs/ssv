package instance

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
)

// Compact trims the given qbft.State down to the minimum required
// for consensus to proceed.
//
// Compact always discards message from previous rounds.
// Compact discards non-commit messages from below their current round, only if the state is decided.
// Compact discards commit messages from below the current round, only if the whole committee has signed.
//
// This helps reduce the state's memory footprint.
func Compact(state *specqbft.State, decidedMessage *specqbft.SignedMessage) {
	compactContainer(state.ProposeContainer, state.Round, state.Decided)
	compactContainer(state.PrepareContainer, state.LastPreparedRound, state.Decided)
	compactContainer(state.RoundChangeContainer, state.Round, state.Decided)

	// Only discard commit messages if the whole committee has signed,
	// otherwise just trim down to the current round and future rounds.
	var wholeCommitteeDecided bool
	if state.Share == nil {
		// Share may be missing in tests.
	} else {
		var signers []spectypes.OperatorID
		if decidedMessage != nil {
			signers = decidedMessage.Signers
		} else if state.Decided && len(state.CommitContainer.Msgs) >= len(state.Share.Committee) {
			signers, _ = state.CommitContainer.LongestUniqueSignersForRoundAndValue(state.Round, state.DecidedValue)
		}
		wholeCommitteeDecided = len(signers) == len(state.Share.Committee)
	}
	compactContainer(state.CommitContainer, state.Round, wholeCommitteeDecided)
}

func compactContainer(container *specqbft.MsgContainer, currentRound specqbft.Round, clear bool) {
	switch {
	case container == nil || len(container.Msgs) == 0:
		// Empty already.
	case clear:
		// Discard all messages.
		container.Msgs = map[specqbft.Round][]*specqbft.SignedMessage{}
	default:
		// Trim down to the current and future rounds.
		for r := range container.Msgs {
			if r < currentRound {
				delete(container.Msgs, r)
			}
		}
	}
}

// CompactCopy is identical to Compact, but instead of modifying the given state,
// it returns a modified copy.
func CompactCopy(state *specqbft.State, decidedMessage *specqbft.SignedMessage) *specqbft.State {
	compact := *state
	compact.ProposeContainer = compactContainerCopy(state.ProposeContainer, state.Round, state.Decided)
	compact.PrepareContainer = compactContainerCopy(state.PrepareContainer, state.LastPreparedRound, state.Decided)
	compact.RoundChangeContainer = compactContainerCopy(state.RoundChangeContainer, state.Round, state.Decided)

	// Only discard commit messages if the whole committee has signed,
	// otherwise just trim down to the current round and future rounds.
	var wholeCommitteeDecided bool
	if state.Share == nil {
		// Share may be missing in tests.
	} else {
		var signers []spectypes.OperatorID
		if decidedMessage != nil {
			signers = decidedMessage.Signers
		} else if state.Decided && len(state.CommitContainer.Msgs) >= len(state.Share.Committee) {
			signers, _ = state.CommitContainer.LongestUniqueSignersForRoundAndValue(state.Round, state.DecidedValue)
		}
		wholeCommitteeDecided = len(signers) == len(state.Share.Committee)
	}
	compact.CommitContainer = compactContainerCopy(state.CommitContainer, state.Round, wholeCommitteeDecided)

	return &compact
}

func compactContainerCopy(container *specqbft.MsgContainer, currentRound specqbft.Round, clear bool) *specqbft.MsgContainer {
	if container == nil {
		return nil
	}
	compact := *container
	compact.Msgs = map[specqbft.Round][]*specqbft.SignedMessage{}
	switch {
	case len(container.Msgs) == 0:
		// Empty already.
	case clear:
		// Discard all messages.
	default:
		// Trim down to the current and future rounds.
		for r, msgs := range container.Msgs {
			if r >= currentRound {
				compact.Msgs[r] = msgs
			}
		}
	}
	return &compact
}
