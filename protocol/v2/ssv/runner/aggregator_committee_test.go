package runner

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

// faultyAggregateSubmitBeacon wraps the testing beacon node and forces the aggregate-selection-proof
// submission to fail, so we can exercise the terminal post-quorum "submit failed" path in
// AggregatorCommitteeRunner.ProcessPostConsensus. All other beacon behavior is inherited unchanged
// via the embedded *BeaconNodeWrapped (method promotion).
type faultyAggregateSubmitBeacon struct {
	*protocoltesting.BeaconNodeWrapped
	submitErr error
}

func (b *faultyAggregateSubmitBeacon) SubmitSignedAggregateSelectionProof(_ context.Context, _ *spec.VersionedSignedAggregateAndProof) error {
	return b.submitErr
}

type aggregatorCommitteeRunnerEnv struct {
	logger    *zap.Logger
	runner    *AggregatorCommitteeRunner
	network   *protocoltesting.TestingNetwork
	keySetMap map[phase0.ValidatorIndex]*spectestingutils.TestKeySet
	sampleKey *spectestingutils.TestKeySet
}

// newAggregatorCommitteeRunnerEnv builds an AggregatorCommitteeRunner wired to the supplied beacon
// node, mirroring newCommitteeRunnerEnv. The beacon is injected so a test can substitute a faulty one.
func newAggregatorCommitteeRunnerEnv(
	t *testing.T,
	validatorIndices []int,
	beaconNode beacon.BeaconNode,
) *aggregatorCommitteeRunnerEnv {
	t.Helper()

	keySetMap := spectestingutils.KeySetMapForValidatorIndexList(validatorIndices)
	sampleKey := keySetMap[phase0.ValidatorIndex(validatorIndices[0])]
	shareMap := spectestingutils.ShareMapFromKeySetMap(keySetMap)

	msgID := spectestingutils.AggregatorCommitteeMsgIDForKeySet(sampleKey)
	network := protocoltesting.NewTestingNetwork(1, sampleKey.OperatorKeys[1])
	logger := zaptest.NewLogger(t)
	signer := ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager())

	config := protocoltesting.TestingConfig(logger, sampleKey)
	config.Network = network
	ctrl := protocoltesting.NewTestingQBFTController(
		sampleKey,
		msgID[:],
		spectestingutils.TestingCommitteeMember(sampleKey),
		config,
		false,
	)

	runnerI, err := NewAggregatorCommitteeRunner(AggregatorCommitteeRunnerOptions{
		BaseRunnerOptions: BaseRunnerOptions{
			NetworkConfig:  networkconfig.TestNetwork,
			Share:          shareMap,
			Beacon:         beaconNode,
			Network:        network,
			Signer:         signer,
			OperatorSigner: spectestingutils.NewOperatorSigner(sampleKey, 1),
		},
		QBFTController: ctrl,
	})
	require.NoError(t, err)

	arunner := runnerI.(*AggregatorCommitteeRunner)
	arunner.SetQBFTRoundTimerF(func(_ context.Context, _ *zap.Logger, _ phase0.Slot) ssv.QBFTRoundTimer {
		return roundtimer.NewTestingTimer()
	})

	return &aggregatorCommitteeRunnerEnv{
		logger:    logger,
		runner:    arunner,
		network:   network,
		keySetMap: keySetMap,
		sampleKey: sampleKey,
	}
}

// startAndFeedThroughConsensus starts the duty and feeds the pre-consensus and consensus messages from
// the spec fixtures (everything except post-consensus), leaving the runner decided and ready to process
// post-consensus. It swaps in a fresh conclusion channel so the caller can observe the duty outcome
// deterministically instead of racing the deadline-watcher goroutine StartNewDuty spawned. It returns
// that channel.
func (e *aggregatorCommitteeRunnerEnv) startAndFeedThroughConsensus(t *testing.T, ctx context.Context, duty *spectypes.AggregatorCommitteeDuty, version spec.DataVersion) chan dutyConclusion {
	t.Helper()

	require.NoError(t, e.runner.StartNewDuty(ctx, e.logger, duty, e.sampleKey.Threshold))

	concluded := make(chan dutyConclusion, 1)
	e.runner.dutyConcluded = concluded

	for _, msg := range spectestingutils.AggregatorCommitteeInputForDuty(duty, e.keySetMap, version) {
		switch msg.SSVMessage.MsgType {
		case spectypes.SSVConsensusMsgType:
			require.NoError(t, e.runner.ProcessConsensus(ctx, e.logger, msg))
		case spectypes.SSVPartialSignatureMsgType:
			psig := &spectypes.PartialSignatureMessages{}
			require.NoError(t, psig.Decode(msg.SSVMessage.Data))
			if psig.Type != spectypes.PostConsensusPartialSig {
				require.NoError(t, e.runner.ProcessPreConsensus(ctx, e.logger, psig))
			}
		default:
		}
	}
	return concluded
}

// postConsensusMsgsFromFixture returns the (valid) post-consensus PartialSignatureMessages from signers
// 1..3 for the duty — the same messages AggregatorCommitteeInputForDuty carries.
func postConsensusMsgsFromFixture(duty *spectypes.AggregatorCommitteeDuty, ksMap map[phase0.ValidatorIndex]*spectestingutils.TestKeySet, version spec.DataVersion) []*spectypes.PartialSignatureMessages {
	msgs := make([]*spectypes.PartialSignatureMessages, 0, 3)
	for id := spectypes.OperatorID(1); id <= 3; id++ {
		msgs = append(msgs, spectestingutils.PostConsensusAggregatorCommitteeMsgForDuty(duty, ksMap, id, version))
	}
	return msgs
}

// TestAggregatorCommitteeRunnerProcessPostConsensus_MarksFailedOnSubmitError asserts that a beacon
// submit failure (a terminal post-quorum error) concludes the duty as failed — not left to surface
// as a false "stuck".
func TestAggregatorCommitteeRunnerProcessPostConsensus_MarksFailedOnSubmitError(t *testing.T) {
	ctx := t.Context()
	const version = spec.DataVersionElectra

	base := protocoltesting.NewTestingBeaconNodeWrapped().(*protocoltesting.BeaconNodeWrapped)
	submitErr := errors.New("beacon rejected aggregate")
	faulty := &faultyAggregateSubmitBeacon{BeaconNodeWrapped: base, submitErr: submitErr}

	env := newAggregatorCommitteeRunnerEnv(t, []int{1}, faulty)
	duty := spectestingutils.TestingAggregatorCommitteeDutyForValidators([]int{1}, []int{}, version)

	concluded := env.startAndFeedThroughConsensus(t, ctx, duty, version)

	var postConsensusErr error
	for _, psig := range postConsensusMsgsFromFixture(duty, env.keySetMap, version) {
		if err := env.runner.ProcessPostConsensus(ctx, env.logger, psig); err != nil {
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

// TestAggregatorCommitteeRunnerProcessPostConsensus_DoesNotMarkFailedOnInvalidSigs asserts that the
// recoverable reconstruct-invalid-signatures case is NOT concluded failed: the root can later re-cross
// quorum on a subsequent message, so concluding here would mask a duty that still completes.
func TestAggregatorCommitteeRunnerProcessPostConsensus_DoesNotMarkFailedOnInvalidSigs(t *testing.T) {
	ctx := t.Context()
	const version = spec.DataVersionElectra

	base := protocoltesting.NewTestingBeaconNodeWrapped().(*protocoltesting.BeaconNodeWrapped)
	env := newAggregatorCommitteeRunnerEnv(t, []int{1}, base)
	duty := spectestingutils.TestingAggregatorCommitteeDutyForValidators([]int{1}, []int{}, version)

	concluded := env.startAndFeedThroughConsensus(t, ctx, duty, version)

	// Corrupt the beacon partial signatures. For the aggregator role, ValidatePostConsensusMsg only
	// checks message structure (not the beacon sigs), so these still reach optimistic quorum, then
	// ReconstructBeaconSig fails → the runner reports PostConsensusQuorumWithInvalidSignatures.
	var postConsensusErr error
	for _, psig := range postConsensusMsgsFromFixture(duty, env.keySetMap, version) {
		for _, m := range psig.Messages {
			m.PartialSignature = bytes.Repeat([]byte{0xEE}, len(m.PartialSignature))
		}
		if err := env.runner.ProcessPostConsensus(ctx, env.logger, psig); err != nil {
			postConsensusErr = err
		}
	}

	require.Error(t, postConsensusErr, "invalid signatures should surface an error")
	var specErr *spectypes.Error
	require.ErrorAs(t, postConsensusErr, &specErr)
	require.Equal(t, spectypes.PostConsensusQuorumWithInvalidSignatures, specErr.Code,
		"invalid-sigs must carry the recoverable spec code")

	require.Empty(t, concluded, "a recoverable invalid-sigs error must NOT conclude the duty")
	require.False(t, env.runner.State.Succeeded)
}

// TestAggregatorCommitteeRunnerProcessPostConsensus_RecoverableInvalidSigsThenSucceeds is the
// regression test for the last coverage gap on #2919/#2918: a quorum that optimistically crosses
// threshold but contains one non-deserializable partial signature must NOT conclude the duty (the
// offending signer is dropped by FallBackAndVerifyEachSignature, so the root falls back below
// quorum and the duty is left un-marked, recoverable), and a subsequent valid partial signature that
// re-crosses quorum must let the duty conclude succeeded. This mirrors
// TestCommitteeRunnerProcessPostConsensus_RecoverableInvalidSigsThenSucceeds
// (committee_postconsensus_classification_test.go) for the aggregator-committee runner, closing the
// gap left by TestAggregatorCommitteeRunnerProcessPostConsensus_DoesNotMarkFailedOnInvalidSigs above,
// which only proves the "stays un-marked" half and never re-attempts a successful retry.
func TestAggregatorCommitteeRunnerProcessPostConsensus_RecoverableInvalidSigsThenSucceeds(t *testing.T) {
	ctx := t.Context()
	const version = spec.DataVersionElectra

	// A healthy (non-faulty) beacon node: submission on the retry round must actually succeed.
	base := protocoltesting.NewTestingBeaconNodeWrapped().(*protocoltesting.BeaconNodeWrapped)
	env := newAggregatorCommitteeRunnerEnv(t, []int{1}, base)
	duty := spectestingutils.TestingAggregatorCommitteeDutyForValidators([]int{1}, []int{}, version)

	concluded := env.startAndFeedThroughConsensus(t, ctx, duty, version)

	// Round 1: signers 1 and 2 send valid post-consensus partial sigs; signer 3 sends a
	// non-deserializable one. ValidatePostConsensusMsg only checks message structure (not the beacon
	// signature), so all three enter the container and cross quorum (3-of-4) optimistically. Then
	// ReconstructBeaconSig fails on the garbage signature, FallBackAndVerifyEachSignature drops only
	// signer 3, and the root falls back below quorum (2-of-4) — recoverable, nothing submitted, and
	// the duty must NOT be concluded.
	msg1 := spectestingutils.PostConsensusAggregatorCommitteeMsgForDuty(duty, env.keySetMap, 1, version)
	require.NoError(t, env.runner.ProcessPostConsensus(ctx, env.logger, msg1))

	msg2 := spectestingutils.PostConsensusAggregatorCommitteeMsgForDuty(duty, env.keySetMap, 2, version)
	require.NoError(t, env.runner.ProcessPostConsensus(ctx, env.logger, msg2))

	badMsg3 := spectestingutils.PostConsensusAggregatorCommitteeMsgForDuty(duty, env.keySetMap, 3, version)
	for _, m := range badMsg3.Messages {
		m.PartialSignature = bytes.Repeat([]byte{0xEE}, len(m.PartialSignature))
	}
	recoverableErr := env.runner.ProcessPostConsensus(ctx, env.logger, badMsg3)
	require.Error(t, recoverableErr, "a quorum with invalid signatures should surface an error")
	var specErr *spectypes.Error
	require.ErrorAs(t, recoverableErr, &specErr)
	require.Equal(t, spectypes.PostConsensusQuorumWithInvalidSignatures, specErr.Code,
		"invalid-sigs must carry the recoverable spec code")

	// The recoverable case must NOT conclude the duty: nothing has been sent on the conclusion channel.
	require.Empty(t, concluded, "a recoverable invalid-sigs error must NOT conclude the duty")
	require.False(t, env.runner.State.Succeeded)

	// Round 2: a subsequent valid partial signature from signer 4 re-crosses quorum with three good
	// sigs (1, 2, 4) → reconstruction succeeds → the duty submits and concludes succeeded.
	msg4 := spectestingutils.PostConsensusAggregatorCommitteeMsgForDuty(duty, env.keySetMap, 4, version)
	require.NoError(t, env.runner.ProcessPostConsensus(ctx, env.logger, msg4))

	require.True(t, env.runner.State.Succeeded, "a valid quorum after recovery must conclude succeeded")

	select {
	case c := <-concluded:
		require.Equal(t, dutyOutcomeSucceeded, c.outcome, "recovery must conclude the duty succeeded, not failed")
	default:
		t.Fatal("expected a succeeded duty conclusion after recovery, got none")
	}
}

// TestAggregatorCommitteeRunnerProcessPostConsensus_TerminalWinsOverConcurrentRecoverable is the
// regression test for the terminalErr/recoverableErr split: within a single ProcessPostConsensus
// call, one validator's post-consensus signatures reconstruct fine but then fail to submit (terminal,
// set from the submit loop after the roots loop), while a second validator's signatures are corrupted
// and reconstruct-fails with the recoverable PostConsensusQuorumWithInvalidSignatures code (set from
// the errCh receive site during the roots loop). Both land in the same call, so which one is observed
// first by the listener select is not controlled by the test. Before the split, a single
// last-write-wins `executionErr` made the final classification depend on that arrival order; the
// fixed code always classifies the duty failed here because terminalErr is checked first,
// independent of goroutine/channel scheduling. We assert on the conclusion outcome, not on which
// error message ends up attached (that part is still last-write-wins by design for same-class
// errors).
func TestAggregatorCommitteeRunnerProcessPostConsensus_TerminalWinsOverConcurrentRecoverable(t *testing.T) {
	ctx := t.Context()
	const version = spec.DataVersionElectra

	base := protocoltesting.NewTestingBeaconNodeWrapped().(*protocoltesting.BeaconNodeWrapped)
	submitErr := errors.New("beacon rejected aggregate")
	faulty := &faultyAggregateSubmitBeacon{BeaconNodeWrapped: base, submitErr: submitErr}

	env := newAggregatorCommitteeRunnerEnv(t, []int{1, 2}, faulty)
	duty := spectestingutils.TestingAggregatorCommitteeDutyForValidators([]int{1, 2}, []int{}, version)

	concluded := env.startAndFeedThroughConsensus(t, ctx, duty, version)

	var postConsensusErr error
	for _, psig := range postConsensusMsgsFromFixture(duty, env.keySetMap, version) {
		// Corrupt only validator 2's signature in this signer's batch: validator 1 keeps a valid
		// signature (reconstructs fine, submits, and fails there — terminal), validator 2's
		// reconstruct fails outright (recoverable).
		for _, m := range psig.Messages {
			if m.ValidatorIndex == phase0.ValidatorIndex(2) {
				m.PartialSignature = bytes.Repeat([]byte{0xEE}, len(m.PartialSignature))
			}
		}
		if err := env.runner.ProcessPostConsensus(ctx, env.logger, psig); err != nil {
			postConsensusErr = err
		}
	}

	require.Error(t, postConsensusErr, "a terminal error concurrent with a recoverable one must still surface")

	select {
	case c := <-concluded:
		require.Equal(t, dutyOutcomeFailed, c.outcome,
			"terminal must win deterministically over a concurrent recoverable error in the same round")
	default:
		t.Fatal("expected a failed duty conclusion, got none")
	}
	require.False(t, env.runner.State.Succeeded, "a failed duty must not be marked succeeded")
}

// TestAggregatorCommitteeRunnerProcessPostConsensus_DrainsBufferedErrCh is a best-effort regression
// test for Ovi finding 1 (the drainErrCh block): when the reconstruct goroutines' errCh sends race
// against signatureCh's close, a buffered recoverable error could previously be skipped by the
// listener select (which may take the "signatureCh closed" branch over a simultaneously-ready errCh
// receive), silently discarding the error and letting ProcessPostConsensus return nil despite a
// validator's post-consensus signature never having been submitted (a false "stuck", never
// concluded). The fix drains any leftover errCh value right after the listener loop, so the error is
// classified and returned regardless of which branch the race took.
//
// This exercises many validators corrupted in the same call and repeats the scenario to raise the
// odds of hitting the race window on any single run; the assertion (a non-nil, correctly-coded error
// on every iteration) is unconditionally guaranteed by the fix regardless of which branch fires, so
// the test is not flaky when the drain is present — it only has a chance (not a guarantee) of
// reproducing the pre-fix bug on a revert.
func TestAggregatorCommitteeRunnerProcessPostConsensus_DrainsBufferedErrCh(t *testing.T) {
	const version = spec.DataVersionElectra
	aggValidators := []int{1, 2, 3, 4, 5, 6, 7, 8}

	for iter := 0; iter < 20; iter++ {
		ctx := t.Context()
		base := protocoltesting.NewTestingBeaconNodeWrapped().(*protocoltesting.BeaconNodeWrapped)
		env := newAggregatorCommitteeRunnerEnv(t, aggValidators, base)
		duty := spectestingutils.TestingAggregatorCommitteeDutyForValidators(aggValidators, []int{}, version)

		env.startAndFeedThroughConsensus(t, ctx, duty, version)

		var postConsensusErr error
		for _, psig := range postConsensusMsgsFromFixture(duty, env.keySetMap, version) {
			for _, m := range psig.Messages {
				m.PartialSignature = bytes.Repeat([]byte{0xEE}, len(m.PartialSignature))
			}
			if err := env.runner.ProcessPostConsensus(ctx, env.logger, psig); err != nil {
				postConsensusErr = err
			}
		}

		require.Error(t, postConsensusErr, "iteration %d: a fully-corrupted quorum must surface an error, not vanish", iter)
		var specErr *spectypes.Error
		require.ErrorAs(t, postConsensusErr, &specErr, "iteration %d", iter)
		require.Equal(t, spectypes.PostConsensusQuorumWithInvalidSignatures, specErr.Code, "iteration %d", iter)
		require.False(t, env.runner.State.Succeeded, "iteration %d", iter)
	}
}

// TestAggregatorCommitteeRunnerPreConsensus_DuplicateCommitteeAggregators is the regression test for
// #2950: when two validators are selected as aggregators for the SAME beacon committee, the
// selectionLoop in the aggregator-committee pre-consensus path (aggregator_committee.go) must append a
// second AssignedAggregator entry to consensusData.Aggregators for the repeat committee, while
// AggregatorsCommitteeIndexes and AggregatedAttestations keep exactly one entry per unique committee
// (fetched/marshaled once, not once per aggregator). This shape is otherwise only enforced by
// consensusData.Validate() at runtime and by ssv-spec tests, not by anything in this repo.
func TestAggregatorCommitteeRunnerPreConsensus_DuplicateCommitteeAggregators(t *testing.T) {
	ctx := t.Context()
	const version = spec.DataVersionElectra

	base := protocoltesting.NewTestingBeaconNodeWrapped()
	env := newAggregatorCommitteeRunnerEnv(t, []int{1, 2}, base)
	// Validators 1 and 2 both selected as aggregators for the same beacon committee (TestingCommitteeIndex).
	duty := spectestingutils.TestingAggregatorCommitteeDutyMultipleAggregators(version)

	require.NoError(t, env.runner.StartNewDuty(ctx, env.logger, duty, env.sampleKey.Threshold))

	// Feed only pre-consensus messages (skip consensus and post-consensus) so we can inspect exactly
	// what the selectionLoop handed to decide, before any QBFT round-change activity can happen.
	for _, msg := range spectestingutils.AggregatorCommitteeInputForDuty(duty, env.keySetMap, version) {
		if msg.SSVMessage.MsgType != spectypes.SSVPartialSignatureMsgType {
			continue
		}
		psig := &spectypes.PartialSignatureMessages{}
		require.NoError(t, psig.Decode(msg.SSVMessage.Data))
		if psig.Type != spectypes.PostConsensusPartialSig {
			require.NoError(t, env.runner.ProcessPreConsensus(ctx, env.logger, psig))
		}
	}

	require.True(t, env.runner.HasStartedConsensus(), "pre-consensus quorum on two same-committee aggregators must start consensus")
	require.NotNil(t, env.runner.State.RunningInstance, "decide must have set a running QBFT instance")

	consensusData := &spectypes.AggregatorCommitteeConsensusData{}
	require.NoError(t, consensusData.Decode(env.runner.State.RunningInstance.StartValue))

	require.NoError(t, consensusData.Validate(), "the proposed consensus data must satisfy the spec-mandated shape")

	require.Len(t, consensusData.Aggregators, 2, "both same-committee aggregators must appear in Aggregators")
	require.Len(t, consensusData.AggregatorsCommitteeIndexes, 1,
		"a repeated committee must not add a second AggregatorsCommitteeIndexes entry")
	require.Len(t, consensusData.AggregatedAttestations, 1,
		"a repeated committee must not fetch/marshal a second aggregate attestation")

	committeeIndex := consensusData.AggregatorsCommitteeIndexes[0]
	gotValidatorIndexes := make([]phase0.ValidatorIndex, 0, 2)
	for _, agg := range consensusData.Aggregators {
		require.Equal(t, committeeIndex, agg.CommitteeIndex, "both aggregators must reference the single shared committee index")
		gotValidatorIndexes = append(gotValidatorIndexes, agg.ValidatorIndex)
	}
	require.ElementsMatch(t, []phase0.ValidatorIndex{1, 2}, gotValidatorIndexes)

	proofs, err := consensusData.GetAggregateAndProofs()
	require.NoError(t, err)
	require.Len(t, proofs, 2, "GetAggregateAndProofs must return one proof per aggregator, even when they share a committee")
	require.Equal(t, consensusData.Version, proofs[0].Version)
	require.Equal(t, consensusData.Version, proofs[1].Version)

	// Both proofs must wrap the same underlying aggregate attestation (same fetched-once attestation,
	// not two independently-fetched copies), which is a stronger check than comparing the raw
	// AggregatedAttestations bytes above. Compare the Aggregate sub-field's own hash tree root, not the
	// whole AggregateAndProof's root (that also embeds the per-validator AggregatorIndex/SelectionProof,
	// which legitimately differ between the two aggregators — the whole-proof roots are asserted
	// distinct below for exactly that reason). proofs[i].Version is derived from the runner's own fork
	// schedule (not the `version` constant used to build the fixtures), so this stays version-agnostic
	// instead of asserting a hardcoded fork.
	proofRoots := func(p *spec.VersionedAggregateAndProof) (aggregateRoot, wholeRoot [32]byte) {
		t.Helper()
		switch p.Version {
		case spec.DataVersionElectra:
			require.NotNil(t, p.Electra)
			aggregateRoot, err := p.Electra.Aggregate.HashTreeRoot()
			require.NoError(t, err)
			wholeRoot, err := p.Electra.HashTreeRoot()
			require.NoError(t, err)
			return aggregateRoot, wholeRoot
		case spec.DataVersionFulu:
			require.NotNil(t, p.Fulu)
			aggregateRoot, err := p.Fulu.Aggregate.HashTreeRoot()
			require.NoError(t, err)
			wholeRoot, err := p.Fulu.HashTreeRoot()
			require.NoError(t, err)
			return aggregateRoot, wholeRoot
		default:
			t.Fatalf("unexpected aggregate-and-proof version in test: %s", p.Version)
			return [32]byte{}, [32]byte{}
		}
	}
	aggregateRoot0, wholeRoot0 := proofRoots(proofs[0])
	aggregateRoot1, wholeRoot1 := proofRoots(proofs[1])
	require.Equal(t, aggregateRoot0, aggregateRoot1,
		"both aggregators' proofs must wrap the same aggregate attestation")
	require.NotEqual(t, wholeRoot0, wholeRoot1,
		"whole-proof roots must differ via per-validator AggregatorIndex/SelectionProof; identical roots would mean both entries collapsed into the same proof")
}
