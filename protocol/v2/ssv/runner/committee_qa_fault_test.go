package runner

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
	"github.com/ssvlabs/ssv/qa/faults"
)

// The QA faults reshape the committee runner's consensus value. applyGloasVoteFault is the seam
// they share, so it is tested directly: the runner-level plumbing around it is unchanged code.
func TestApplyGloasVoteFault(t *testing.T) {
	newVote := func(index phase0.CommitteeIndex) *gloas.GloasBeaconVote {
		return &gloas.GloasBeaconVote{
			BlockRoot:            phase0.Root{0x01},
			Source:               &phase0.Checkpoint{Epoch: 1},
			Target:               &phase0.Checkpoint{Epoch: 2},
			AttestationDataIndex: index,
		}
	}

	t.Run("no fault leaves the gloas vote in place", func(t *testing.T) {
		vote := newVote(1)
		input, permissive := applyGloasVoteFault(nil, vote, 100)
		require.Same(t, vote, input)
		require.False(t, permissive)
		require.Equal(t, phase0.CommitteeIndex(1), vote.AttestationDataIndex)
	})

	t.Run("vote-112b swaps in a pre-gloas 112 byte vote", func(t *testing.T) {
		faults.SetForTest(t, faults.Vote112B)
		vote := newVote(1)

		input, permissive := applyGloasVoteFault(nil, vote, 100)

		require.True(t, permissive)
		pre, ok := input.(*spectypes.BeaconVote)
		require.True(t, ok, "expected a pre-Gloas BeaconVote, got %T", input)
		require.Equal(t, vote.BlockRoot, pre.BlockRoot)
		encoded, err := pre.Encode()
		require.NoError(t, err)
		require.Len(t, encoded, 112)
	})

	t.Run("vote-index-2 sets an out of range index", func(t *testing.T) {
		faults.SetForTest(t, faults.VoteIndex2)
		vote := newVote(1)

		input, permissive := applyGloasVoteFault(nil, vote, 100)

		require.True(t, permissive)
		require.Same(t, vote, input)
		require.Equal(t, phase0.CommitteeIndex(2), vote.AttestationDataIndex)
	})

	t.Run("vote-index-flip keeps the index valid", func(t *testing.T) {
		faults.SetForTest(t, faults.VoteIndexFlip)

		flipped := newVote(0)
		input, permissive := applyGloasVoteFault(nil, flipped, 100)
		require.False(t, permissive, "a flipped index is still valid, so the honest check must stay")
		require.Same(t, flipped, input)
		require.Equal(t, phase0.CommitteeIndex(1), flipped.AttestationDataIndex)

		back := newVote(1)
		_, _ = applyGloasVoteFault(nil, back, 100)
		require.Equal(t, phase0.CommitteeIndex(0), back.AttestationDataIndex)
	})
}

func TestFlipAttestationIndex(t *testing.T) {
	require.Equal(t, phase0.CommitteeIndex(1), flipAttestationIndex(0))
	require.Equal(t, phase0.CommitteeIndex(0), flipAttestationIndex(1))
	// Anything outside the valid range maps to 0, so the second signature is always a genuine
	// double vote on a well-formed value rather than a malformed one.
	require.Equal(t, phase0.CommitteeIndex(0), flipAttestationIndex(7))
}
