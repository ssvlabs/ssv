package runner

import (
	"bytes"
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
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

// invalidateDutiesInGuard marks every validator duty of the committee duty invalid in the guard
// stub, so expectedPostConsensusRootsAndBeaconObjects (and the ProcessConsensus signing loop) skips
// them all.
func invalidateDutiesInGuard(guard *committeeDutyGuardStub, duty *spectypes.CommitteeDuty) {
	guard.validErrs = make(map[string]error)
	for _, vd := range duty.ValidatorDuties {
		key := guard.validKey(vd.Type, spectypes.ValidatorPK(vd.PubKey), vd.DutySlot())
		guard.validErrs[key] = errors.New("duty no longer valid")
	}
}

// TestCommitteeRunnerProcessPostConsensus_MarksNotRequiredOnNoBeaconObjects is the regression test
// for #2903: a post-consensus quorum where this operator ends up with no beacon objects to submit
// (e.g. divergent validator sets across the committee's operators — modeled here by invalidating
// the duties in the guard after consensus) is a benign terminal. It must conclude not_required —
// not failed (the previous behavior, surfacing as a spurious "⚠️ duty failed") and not a silent
// stall — while still surfacing the sentinel for the queue's terminal-drop handling.
func TestCommitteeRunnerProcessPostConsensus_MarksNotRequiredOnNoBeaconObjects(t *testing.T) {
	guard := &committeeDutyGuardStub{}
	env := newCommitteeRunnerEnv(t, []int{1}, guard, &doppelgangerStub{})
	duty := spectestingutils.TestingCommitteeDuty([]int{1}, nil, spec.DataVersionElectra)

	env.startAndDecideCommitteeDuty(t, duty)
	concluded := observeConclusion(env)

	invalidateDutiesInGuard(guard, duty)

	var postConsensusErr error
	for id := spectypes.OperatorID(1); id <= 3; id++ {
		msg := spectestingutils.PostConsensusCommitteeMsgForDuty(duty, env.keySetMap, id)
		if err := env.runner.ProcessPostConsensus(context.Background(), env.logger, msg); err != nil {
			postConsensusErr = err
		}
	}

	require.ErrorIs(t, postConsensusErr, ErrNoValidDutiesToExecute, "the benign sentinel must surface to the queue")

	select {
	case c := <-concluded:
		require.Equal(t, dutyOutcomeNotRequired, c.outcome, "no beacon objects to submit must conclude not_required, not failed")
		require.NoError(t, c.reason)
	default:
		t.Fatal("expected a not_required duty conclusion, got none")
	}
	require.True(t, env.runner.State.Succeeded, "not_required is a correct completion")
	require.Empty(t, env.beacon.GetBroadcastedRoots(), "nothing should have been submitted")
}

// faultyDomainDataBeacon wraps the testing beacon node with a switchable DomainData failure, so a
// test can let consensus-phase signing succeed and then fail every post-consensus beacon-object
// construction. All other behavior is inherited via the embedded *BeaconNodeWrapped.
type faultyDomainDataBeacon struct {
	*protocoltesting.BeaconNodeWrapped
	domainErr error
	fail      atomic.Bool
}

func (b *faultyDomainDataBeacon) DomainData(ctx context.Context, epoch phase0.Epoch, domain phase0.DomainType) (phase0.Domain, error) {
	if b.fail.Load() {
		return phase0.Domain{}, b.domainErr
	}
	return b.BeaconNodeWrapped.DomainData(ctx, epoch, domain)
}

// TestCommitteeRunnerProcessPostConsensus_MarksFailedOnAllConstructionFailures guards the boundary of
// the not_required classification: an empty beacon-objects map caused by every per-validator
// construction failing (modeled by DomainData failing after consensus) is a MISSED submission, not a
// benign no-op — expectedPostConsensusRootsAndBeaconObjects must surface the error so the duty
// concludes failed instead of not_required.
func TestCommitteeRunnerProcessPostConsensus_MarksFailedOnAllConstructionFailures(t *testing.T) {
	base := protocoltesting.NewTestingBeaconNodeWrapped().(*protocoltesting.BeaconNodeWrapped)
	domainErr := errors.New("domain data unavailable")
	faulty := &faultyDomainDataBeacon{BeaconNodeWrapped: base, domainErr: domainErr}

	env := newCommitteeRunnerEnvWithBeacon(t, []int{1}, faulty)
	duty := spectestingutils.TestingCommitteeDuty([]int{1}, nil, spec.DataVersionElectra)

	env.startAndDecideCommitteeDuty(t, duty)
	concluded := observeConclusion(env)

	// Consensus-phase signing has already fetched domain data successfully; from here on every
	// post-consensus object construction fails, emptying the beacon-objects map for a duty this
	// operator WAS supposed to submit.
	faulty.fail.Store(true)

	var postConsensusErr error
	for id := spectypes.OperatorID(1); id <= 3; id++ {
		msg := spectestingutils.PostConsensusCommitteeMsgForDuty(duty, env.keySetMap, id)
		if err := env.runner.ProcessPostConsensus(context.Background(), env.logger, msg); err != nil {
			postConsensusErr = err
		}
	}

	require.ErrorIs(t, postConsensusErr, domainErr, "the construction failure must surface, not be swallowed into an empty map")
	require.NotErrorIs(t, postConsensusErr, ErrNoValidDutiesToExecute, "an all-construction-failure empty map must not be classified as the benign sentinel")

	select {
	case c := <-concluded:
		require.Equal(t, dutyOutcomeFailed, c.outcome, "a missed submission must conclude failed, not not_required")
		require.ErrorIs(t, c.reason, domainErr)
	default:
		t.Fatal("expected a failed duty conclusion, got none")
	}
	require.False(t, env.runner.State.Succeeded, "a missed submission must not be marked succeeded")
}

// TestCommitteeRunnerProcessConsensus_MarksNotRequiredOnNoValidDuties covers the consensus-phase
// sibling of the #2903 sentinel: a committee that decides while this operator has zero valid duties
// to sign (all invalidated in the guard before consensus) previously concluded via no marker at
// all, surfacing as a false "stuck". It must conclude not_required and surface the sentinel.
func TestCommitteeRunnerProcessConsensus_MarksNotRequiredOnNoValidDuties(t *testing.T) {
	guard := &committeeDutyGuardStub{}
	env := newCommitteeRunnerEnv(t, []int{1}, guard, &doppelgangerStub{})
	duty := spectestingutils.TestingCommitteeDuty([]int{1}, nil, spec.DataVersionElectra)

	ctx := t.Context()
	require.NoError(t, env.runner.StartNewDuty(ctx, env.logger, duty, env.sampleKey.Threshold))
	concluded := observeConclusion(env)

	invalidateDutiesInGuard(guard, duty)

	var consensusErr error
	for _, msg := range spectestingutils.CommitteeInputForDuty(duty, duty.Slot, env.keySetMap, false) {
		if err := env.runner.ProcessConsensus(ctx, env.logger, msg); err != nil {
			consensusErr = err
		}
	}

	require.ErrorIs(t, consensusErr, ErrNoValidDutiesToExecute, "the benign sentinel must surface to the queue")

	select {
	case c := <-concluded:
		require.Equal(t, dutyOutcomeNotRequired, c.outcome, "deciding with zero valid duties must conclude not_required")
		require.NoError(t, c.reason)
	default:
		t.Fatal("expected a not_required duty conclusion, got none")
	}
	require.True(t, env.runner.State.Succeeded, "not_required is a correct completion")
}
