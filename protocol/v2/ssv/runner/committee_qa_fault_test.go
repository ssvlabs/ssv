package runner

import (
	"context"
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/networkconfig"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
	"github.com/ssvlabs/ssv/qa/faults"
)

// The QA faults reshape the committee runner's consensus value. applyGloasVoteFault is the seam
// they share, so it is tested directly; TestGloasVoteFaultStartsInstanceOnSameSlotHead covers the
// runner-level plumbing around it.
func TestApplyGloasVoteFault(t *testing.T) {
	newVote := func(index phase0.CommitteeIndex) *gloas.GloasBeaconVote {
		return &gloas.GloasBeaconVote{
			BlockRoot:            phase0.Root{0x01},
			Source:               &phase0.Checkpoint{Epoch: 1},
			Target:               &phase0.Checkpoint{Epoch: 2},
			AttestationDataIndex: index,
		}
	}
	sameSlot := func(root phase0.Root) *phase0.Root { return &root }

	t.Run("no fault leaves the gloas vote in place", func(t *testing.T) {
		vote := newVote(1)
		input, override := applyGloasVoteFault(nil, vote, 100, sameSlot(vote.BlockRoot))
		require.Same(t, vote, input)
		require.Equal(t, faults.KeepHonestCheck, override)
		require.Equal(t, phase0.CommitteeIndex(1), vote.AttestationDataIndex)
	})

	t.Run("vote-112b swaps in a pre-gloas 112 byte vote", func(t *testing.T) {
		faults.SetForTest(t, faults.Vote112B)
		vote := newVote(1)

		input, override := applyGloasVoteFault(nil, vote, 100, nil)

		require.Equal(t, faults.PermissiveCheck, override)
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

		input, override := applyGloasVoteFault(nil, vote, 100, nil)

		require.Equal(t, faults.PermissiveCheck, override)
		require.Same(t, vote, input)
		require.Equal(t, phase0.CommitteeIndex(2), vote.AttestationDataIndex)
	})

	t.Run("vote-index-flip flips the index and drops the same-slot view", func(t *testing.T) {
		faults.SetForTest(t, faults.VoteIndexFlip)

		flipped := newVote(0)
		input, override := applyGloasVoteFault(nil, flipped, 100, sameSlot(flipped.BlockRoot))
		require.Equal(t, faults.DropSameSlotView, override, "the faulted node's own view would refuse index 1 (A.0)")
		require.Same(t, flipped, input)
		require.Equal(t, phase0.CommitteeIndex(1), flipped.AttestationDataIndex)
		require.Equal(t, phase0.Root{0x01}, flipped.BlockRoot, "flip never touches the root")

		back := newVote(1)
		_, override = applyGloasVoteFault(nil, back, 100, nil)
		require.Equal(t, faults.DropSameSlotView, override)
		require.Equal(t, phase0.CommitteeIndex(0), back.AttestationDataIndex)
	})

	t.Run("vote-index-1-same-slot, view matches the head", func(t *testing.T) {
		faults.SetForTest(t, faults.VoteIndex1SameSlot)
		vote := newVote(0)

		input, override := applyGloasVoteFault(nil, vote, 100, sameSlot(vote.BlockRoot))

		require.Equal(t, faults.DropSameSlotView, override)
		require.Same(t, vote, input)
		require.Equal(t, phase0.CommitteeIndex(1), vote.AttestationDataIndex)
		require.Equal(t, phase0.Root{0x01}, vote.BlockRoot)
	})

	t.Run("vote-index-1-same-slot, no view", func(t *testing.T) {
		faults.SetForTest(t, faults.VoteIndex1SameSlot)
		vote := newVote(0)

		input, override := applyGloasVoteFault(nil, vote, 100, nil)

		require.Equal(t, faults.KeepHonestCheck, override, "nothing a follower could reject on, so nothing to weaken")
		require.Same(t, vote, input)
		require.Equal(t, *newVote(0), *vote, "the vote is left unchanged")
	})

	t.Run("vote-index-1-same-slot, view names another root", func(t *testing.T) {
		faults.SetForTest(t, faults.VoteIndex1SameSlot)
		vote := newVote(0)

		input, override := applyGloasVoteFault(nil, vote, 100, sameSlot(phase0.Root{0xbb}))

		require.Equal(t, faults.DropSameSlotView, override)
		require.Same(t, vote, input)
		require.Equal(t, phase0.CommitteeIndex(1), vote.AttestationDataIndex)
		require.Equal(t, phase0.Root{0xbb}, vote.BlockRoot, "the same-slot root is pinned")
	})
}

func TestFlipAttestationIndex(t *testing.T) {
	require.Equal(t, phase0.CommitteeIndex(1), flipAttestationIndex(0))
	require.Equal(t, phase0.CommitteeIndex(0), flipAttestationIndex(1))
	// Anything outside the valid range maps to 0, so the second signature is always a genuine
	// double vote on a well-formed value rather than a malformed one.
	require.Equal(t, phase0.CommitteeIndex(0), flipAttestationIndex(7))
}

// newSameSlotFaultEnv is a committee runner at a Gloas slot whose block has arrived: its beacon node
// attests to that block (root) with payload EMPTY and its head events name root for the slot, so the
// runner holds a same-slot view at instance start (the sameSlotBeacon harness, every slot on ssv-mini).
func newSameSlotFaultEnv(t *testing.T) (*committeeRunnerEnv, spectypes.Duty, phase0.Root) {
	t.Helper()
	duty := spectestingutils.TestingAttesterDuty(spec.DataVersionElectra)
	root := phase0.Root{0xaa}
	beacon := &sameSlotBeacon{BeaconNodeWrapped: protocoltesting.NewTestingBeaconNodeWrapped().(*protocoltesting.BeaconNodeWrapped), root: root}
	beacon.HeadRoots = map[phase0.Slot]phase0.Root{duty.Slot: root}
	env := newCommitteeRunnerEnvWithBeacon(t, []int{1}, beacon)
	env.runner.NetworkConfig = networkconfig.TestNetworkWithGloas(0)
	env.runner.State = NewRunnerState(env.sampleKey.Threshold, duty)
	return env, duty, root
}

// A.0 regression guard: since the honest checker holds the operator's own same-slot view, a faulted
// node that kept it would refuse its OWN index-1 value in decide ("input data invalid: gloas
// attestation data index 1 for a same-slot block") and never start the instance, so nothing reached
// the wire. Both index-1 values must start the instance with index 1 as the start value, while the
// faulted node still refuses an out-of-range index from others.
func TestGloasVoteFaultStartsInstanceOnSameSlotHead(t *testing.T) {
	for _, f := range []faults.Fault{faults.VoteIndexFlip, faults.VoteIndex1SameSlot} {
		t.Run(string(f), func(t *testing.T) {
			faults.SetForTest(t, f)
			env, duty, root := newSameSlotFaultEnv(t)

			require.NoError(t, env.runner.executeDuty(context.Background(), env.logger, duty))
			require.NotNil(t, env.runner.State.RunningInstance, "the faulted node must start its own instance")

			var started gloas.GloasBeaconVote
			require.NoError(t, started.Decode(env.runner.State.RunningInstance.StartValue))
			require.Equal(t, phase0.CommitteeIndex(1), started.AttestationDataIndex)
			require.Equal(t, root, started.BlockRoot)

			vote := func(index phase0.CommitteeIndex) []byte {
				b, err := (&gloas.GloasBeaconVote{BlockRoot: root, Source: started.Source, Target: started.Target, AttestationDataIndex: index}).Encode()
				require.NoError(t, err)
				return b
			}
			require.NoError(t, env.runner.ValCheck.CheckValue(vote(1)), "no same-slot view on the faulted node")
			require.ErrorContains(t, env.runner.ValCheck.CheckValue(vote(2)), "must be 0 or 1")

			// Honest side: a FAULT=none runner on the same view refuses the faulted start value with the
			// same-slot string, the ATT-10 / FLT-05 leg A oracle.
			faults.SetForTest(t, faults.None)
			honest, honestDuty, _ := newSameSlotFaultEnv(t)
			require.NoError(t, honest.runner.executeDuty(context.Background(), honest.logger, honestDuty))
			require.ErrorContains(t, honest.runner.ValCheck.CheckValue(env.runner.State.RunningInstance.StartValue),
				"gloas attestation data index 1 for a same-slot block")
		})
	}
}

// The permissive vote values: the faulted node starts its instance, and an honest (FAULT=none) runner
// refuses the faulted start value with the exact string the runbook oracle greps (A.2, A.3).
func TestGloasPermissiveVoteFaultsHonestOracle(t *testing.T) {
	for _, tc := range []struct {
		fault faults.Fault
		want  []string
	}{
		{faults.Vote112B, []string{"failed decoding gloas beacon vote", "(have 112, needed 120)"}},
		{faults.VoteIndex2, []string{"attestation data index 2 must be 0 or 1"}},
	} {
		t.Run(string(tc.fault), func(t *testing.T) {
			faults.SetForTest(t, tc.fault)
			env, duty, _ := newSameSlotFaultEnv(t)
			require.NoError(t, env.runner.executeDuty(context.Background(), env.logger, duty))
			require.NotNil(t, env.runner.State.RunningInstance)
			require.IsType(t, faults.PermissiveValueCheck{}, env.runner.ValCheck)

			faults.SetForTest(t, faults.None)
			honest, honestDuty, _ := newSameSlotFaultEnv(t)
			require.NoError(t, honest.runner.executeDuty(context.Background(), honest.logger, honestDuty))
			err := honest.runner.ValCheck.CheckValue(env.runner.State.RunningInstance.StartValue)
			for _, w := range tc.want {
				require.ErrorContains(t, err, w)
			}
		})
	}
}
