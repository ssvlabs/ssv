package instance

import (
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
)

// Compact trims the given qbft.State down to the minimum required
// for consensus to proceed.
//
// Compact always discards message from previous rounds.
// Compact discards all non-commit messages, only if the given state is decided.
//
// This helps reduce the state's memory footprint.
func Compact(state *genesisspecqbft.State, decidedMessage *genesisspecqbft.SignedMessage) {
	compact(state, decidedMessage, compactContainerEdit)
}

// CompactCopy returns a compacted copy of the given qbft.State.
// CompactCopy is guaranteed to not modify the given state, but the returned state may be modified
// when the given state is modified.
//
// TODO: this is a temporary solution to not break spec-tests. Revert this once spec is aligned.
//
// See Compact for more details.
func CompactCopy(state *genesisspecqbft.State, decidedMessage *genesisspecqbft.SignedMessage) *genesisspecqbft.State {
	stateCopy := *state
	compact(&stateCopy, decidedMessage, compactContainerCopy)
	return &stateCopy
}

func compact(state *genesisspecqbft.State, decidedMessage *genesisspecqbft.SignedMessage, compactContainer compactContainerFunc) {
	state.ProposeContainer = compactContainer(state.ProposeContainer, state.Round, state.Decided)
	state.PrepareContainer = compactContainer(state.PrepareContainer, state.LastPreparedRound, state.Decided)
	state.RoundChangeContainer = compactContainer(state.RoundChangeContainer, state.Round, state.Decided)
	state.CommitContainer = compactContainer(state.CommitContainer, state.Round, false)

	// TODO: disabled for now as we depend on the commit messages to check for
	// whether we need to save an incoming decided message or not (see UponDecided).
	//
	// // Only discard commit messages if the whole committee has signed,
	// // otherwise just trim down to the current round and future rounds.
	// var wholeCommitteeDecided bool
	// if state.Share == nil {
	// 	// Share may be missing in tests.
	// } else {
	// 	var signers []genesisspectypes.OperatorID
	// 	if decidedMessage != nil {
	// 		signers = decidedMessage.Signers
	// 	} else if state.Decided && len(state.CommitContainer.Msgs) >= len(state.Share.Committee) {
	// 		signers, _ = state.CommitContainer.LongestUniqueSignersForRoundAndValue(state.Round, state.DecidedValue)
	// 	}
	// 	wholeCommitteeDecided = len(signers) == len(state.Share.Committee)
	// }
	// state.CommitContainer = compactContainer(state.CommitContainer, state.Round, wholeCommitteeDecided)
}

type compactContainerFunc func(container *genesisspecqbft.MsgContainer, currentRound genesisspecqbft.Round, clear bool) *genesisspecqbft.MsgContainer

func compactContainerEdit(container *genesisspecqbft.MsgContainer, currentRound genesisspecqbft.Round, clear bool) *genesisspecqbft.MsgContainer {
	switch {
	case container == nil || len(container.Msgs) == 0:
		// Empty already.
	case clear:
		// Discard all messages.
		container.Msgs = map[genesisspecqbft.Round][]*genesisspecqbft.SignedMessage{}
	default:
		// Trim down to the current and future rounds.
		for r := range container.Msgs {
			if r < currentRound {
				delete(container.Msgs, r)
			}
		}
	}
	return container
}

func compactContainerCopy(container *genesisspecqbft.MsgContainer, currentRound genesisspecqbft.Round, clear bool) *genesisspecqbft.MsgContainer {
	switch {
	case container == nil || len(container.Msgs) == 0:
		// Empty already.
		return container
	case clear:
		// Discard all messages.
		return &genesisspecqbft.MsgContainer{
			Msgs: map[genesisspecqbft.Round][]*genesisspecqbft.SignedMessage{},
		}
	default:
		// Trim down to the current and future rounds.
		compact := genesisspecqbft.MsgContainer{
			Msgs: map[genesisspecqbft.Round][]*genesisspecqbft.SignedMessage{},
		}
		for r, msgs := range container.Msgs {
			if r >= currentRound {
				compact.Msgs[r] = msgs
			}
		}
		return &compact
	}
}
