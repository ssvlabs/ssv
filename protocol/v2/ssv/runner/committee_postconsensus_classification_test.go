package runner

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"

	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
)

// faultyAttestationSubmitBeacon wraps the testing beacon node and forces attestation submission to
// fail, so we can exercise the terminal post-quorum "submit failed" path in
// CommitteeRunner.ProcessPostConsensus. All other beacon behavior is inherited unchanged via the
// embedded *BeaconNodeWrapped (method promotion).
type faultyAttestationSubmitBeacon struct {
	*protocoltesting.BeaconNodeWrapped
	submitErr error
}

func (b *faultyAttestationSubmitBeacon) SubmitAttestations(_ context.Context, _ []*spec.VersionedAttestation) error {
	return b.submitErr
}

// observeConclusion swaps in a fresh, buffered conclusion channel so a test can read the duty's
// terminal outcome deterministically instead of racing the deadline-watcher goroutine StartNewDuty
// spawned. markDuty{Failed,Succeeded} send on the current dutyConcluded field, so the swap must
// happen after StartNewDuty (which spawned the watcher on the original channel) and before the
// ProcessPostConsensus call that concludes the duty.
func observeConclusion(env *committeeRunnerEnv) chan dutyConclusion {
	concluded := make(chan dutyConclusion, 1)
	env.runner.dutyConcluded = concluded
	return concluded
}

// TestCommitteeRunnerProcessPostConsensus_MarksFailedOnSubmitError asserts that a beacon submit
// failure (a terminal post-quorum error) concludes the duty as failed — not left to surface as a
// false "stuck". This is the terminal branch of the terminalErr/recoverableErr split.
func TestCommitteeRunnerProcessPostConsensus_MarksFailedOnSubmitError(t *testing.T) {
	base := protocoltesting.NewTestingBeaconNodeWrapped().(*protocoltesting.BeaconNodeWrapped)
	submitErr := errors.New("beacon rejected attestations")
	faulty := &faultyAttestationSubmitBeacon{BeaconNodeWrapped: base, submitErr: submitErr}

	env := newCommitteeRunnerEnvWithBeacon(t, []int{1}, faulty)
	duty := spectestingutils.TestingCommitteeDuty([]int{1}, nil, spec.DataVersionElectra)

	env.startAndDecideCommitteeDuty(t, duty)
	concluded := observeConclusion(env)

	var postConsensusErr error
	for id := spectypes.OperatorID(1); id <= 3; id++ {
		msg := spectestingutils.PostConsensusCommitteeMsgForDuty(duty, env.keySetMap, id)
		if err := env.runner.ProcessPostConsensus(context.Background(), env.logger, msg); err != nil {
			postConsensusErr = err
		}
	}

	require.ErrorIs(t, postConsensusErr, submitErr, "submit failure should surface from ProcessPostConsensus")

	select {
	case c := <-concluded:
		require.Equal(t, dutyOutcomeFailed, c.outcome, "a terminal submit failure must be classified failed")
		require.ErrorIs(t, c.reason, submitErr)
	default:
		t.Fatal("expected a failed duty conclusion, got none")
	}
	require.False(t, env.runner.State.Succeeded, "a failed duty must not be marked succeeded")
}

// TestCommitteeRunnerProcessPostConsensus_RecoverableInvalidSigsThenSucceeds is the regression test
// for the mis-classification fix: a quorum that contains one non-deserializable partial signature
// must NOT conclude the duty failed (the offending sig is dropped by the fallback, so the root can
// re-cross quorum), and a subsequent valid partial signature that re-crosses quorum must let the duty
// conclude succeeded. This pins both the push-site recoverable tagging and the defer exclusion in a
// single flow.
func TestCommitteeRunnerProcessPostConsensus_RecoverableInvalidSigsThenSucceeds(t *testing.T) {
	env := newCommitteeRunnerEnv(t, []int{1}, &committeeDutyGuardStub{}, &doppelgangerStub{})
	duty := spectestingutils.TestingCommitteeDuty([]int{1}, nil, spec.DataVersionElectra)

	env.startAndDecideCommitteeDuty(t, duty)
	concluded := observeConclusion(env)

	// Signers 1 and 2 send valid post-consensus partial sigs; signer 3 sends a non-deserializable one.
	// Post-consensus validation only checks message structure (not the beacon sig), so all three enter
	// the container and cross quorum optimistically — then ReconstructBeaconSig fails on the garbage
	// sig, FallBackAndVerifyEachSignature drops only signer 3, and the root falls back below quorum.
	msg1 := spectestingutils.PostConsensusCommitteeMsgForDuty(duty, env.keySetMap, 1)
	require.NoError(t, env.runner.ProcessPostConsensus(context.Background(), env.logger, msg1))

	msg2 := spectestingutils.PostConsensusCommitteeMsgForDuty(duty, env.keySetMap, 2)
	require.NoError(t, env.runner.ProcessPostConsensus(context.Background(), env.logger, msg2))

	badMsg := spectestingutils.PostConsensusCommitteeMsgForDuty(duty, env.keySetMap, 3)
	for _, m := range badMsg.Messages {
		m.PartialSignature = bytes.Repeat([]byte{0xEE}, len(m.PartialSignature))
	}
	recoverableErr := env.runner.ProcessPostConsensus(context.Background(), env.logger, badMsg)
	require.Error(t, recoverableErr, "a quorum with invalid signatures should surface an error")

	// The recoverable case must NOT conclude the duty: nothing has been sent on the conclusion channel.
	require.Empty(t, concluded, "a recoverable invalid-sigs error must NOT conclude the duty failed")
	require.False(t, env.runner.State.Succeeded)
	require.Empty(t, env.beacon.GetBroadcastedRoots(), "nothing should have been submitted yet")

	// A subsequent valid partial signature from signer 4 re-crosses quorum with three good sigs
	// (1, 2, 4) → reconstruction succeeds → the duty submits and concludes succeeded.
	msg4 := spectestingutils.PostConsensusCommitteeMsgForDuty(duty, env.keySetMap, 4)
	require.NoError(t, env.runner.ProcessPostConsensus(context.Background(), env.logger, msg4))

	require.True(t, env.runner.State.Succeeded, "a valid quorum after recovery must conclude succeeded")
	require.Len(t, env.beacon.GetBroadcastedRoots(), 1)

	select {
	case c := <-concluded:
		require.Equal(t, dutyOutcomeSucceeded, c.outcome, "recovery must conclude the duty succeeded, not failed")
	default:
		t.Fatal("expected a succeeded duty conclusion after recovery, got none")
	}
}
