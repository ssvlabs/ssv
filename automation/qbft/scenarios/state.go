package scenarios

import (
	"bytes"
	"sort"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
)

// pass states by value to modify them
func matchedStates(actual specqbft.State, expected specqbft.State) bool {
	for round, messages := range expected.CommitContainer.Msgs {
		signers, _ := actual.CommitContainer.LongestUniqueSignersForRoundAndValue(round, messages[0].Message.Data)
		if !actual.Share.HasQuorum(len(signers)) {
			return false
		}
	}

	// already checked above
	actual.CommitContainer = nil
	expected.CommitContainer = nil

	for _, messages := range actual.PrepareContainer.Msgs {
		sort.Slice(messages, func(i, j int) bool {
			return messages[i].Signers[0] < messages[j].Signers[0]
		})
	}

	actualRoot, err := actual.GetRoot()
	if err != nil {
		return false
	}

	expectedRoot, err := expected.GetRoot()
	if err != nil {
		return false
	}

	return bytes.Equal(actualRoot, expectedRoot)
}
