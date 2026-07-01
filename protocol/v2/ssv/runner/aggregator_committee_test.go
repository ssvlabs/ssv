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
	e.runner.BaseRunner.dutyConcluded = concluded

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
