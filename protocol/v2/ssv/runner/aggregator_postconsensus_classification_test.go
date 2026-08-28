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
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/protocol/v2/types/ssvtestingutils"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

// faultyAggregateSubmitBeacon is declared in aggregator_committee_test.go and shared by both
// AggregatorRunner (this file) and AggregatorCommitteeRunner test suites.

// aggregatorRunnerEnv wires a legacy (non-committee) AggregatorRunner. The beacon node is injected
// so a test can substitute a faulty one.
type aggregatorRunnerEnv struct {
	logger  *zap.Logger
	runner  *AggregatorRunner
	network *protocoltesting.TestingNetwork
	keySet  *spectestingutils.TestKeySet
}

func newAggregatorRunnerEnv(t *testing.T, beaconNode beacon.BeaconNode) *aggregatorRunnerEnv {
	t.Helper()

	keySet := spectestingutils.Testing4SharesSet()
	share := spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex)

	network := protocoltesting.NewTestingNetwork(1, keySet.OperatorKeys[1])
	logger := zaptest.NewLogger(t)
	signer := ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager())

	config := protocoltesting.TestingConfig(logger, keySet)
	config.Network = network

	identifier := ssvtestingutils.NewMsgID(spectypes.JatoTestnet, spectestingutils.TestingValidatorPubKey[:], ssvtypes.RoleAggregator)
	ctrl := protocoltesting.NewTestingQBFTController(
		keySet,
		identifier[:],
		spectestingutils.TestingCommitteeMember(keySet),
		config,
		false,
	)

	runnerI, err := NewAggregatorRunner(AggregatorRunnerOptions{
		BaseRunnerOptions: BaseRunnerOptions{
			NetworkConfig:  networkconfig.TestNetwork,
			Share:          map[phase0.ValidatorIndex]*spectypes.Share{share.ValidatorIndex: share},
			Beacon:         beaconNode,
			Network:        network,
			Signer:         signer,
			OperatorSigner: spectestingutils.NewOperatorSigner(keySet, 1),
		},
		QBFTController:     ctrl,
		ValCheck:           dummyValueChecker{},
		HighestDecidedSlot: 0,
	})
	require.NoError(t, err)

	arunner := runnerI.(*AggregatorRunner)
	arunner.SetQBFTRoundTimerF(func(_ context.Context, _ *zap.Logger, _ phase0.Slot) ssv.QBFTRoundTimer {
		return roundtimer.NewTestingTimer()
	})

	return &aggregatorRunnerEnv{
		logger:  logger,
		runner:  arunner,
		network: network,
		keySet:  keySet,
	}
}

// startAndDecideAggregatorDuty starts the duty and drives the QBFT round to decided, bypassing
// pre-consensus entirely (it is orthogonal to the post-consensus classification under test): the
// runner's own decide() is invoked directly with a hand-built ProposerConsensusData wrapping the
// same AggregateAndProof the postConsensusAggregatorMsg fixtures sign, then that value/round is
// carried to a decision using the generic SSVDecidingMsgsV consensus-message builder (role !=
// RoleProposer, so it emits no pre-consensus messages, only proposal/prepare/commit). Feeding those
// messages through the real ProcessConsensus exercises the same code path production traffic would.
func (e *aggregatorRunnerEnv) startAndDecideAggregatorDuty(
	t *testing.T,
	ctx context.Context,
	duty *spectypes.ValidatorDuty,
	version spec.DataVersion,
) {
	t.Helper()

	require.NoError(t, e.runner.StartNewDuty(ctx, e.logger, duty, e.keySet.Threshold))

	aggData := spectestingutils.TestingAggregateAndProofV(version, duty.ValidatorIndex)
	dataSSZ, err := aggData.MarshalSSZ()
	require.NoError(t, err)

	consensusData := &spectypes.ProposerConsensusData{
		Duty:    *duty,
		Version: version,
		DataSSZ: dataSSZ,
	}
	require.NoError(t, e.runner.decide(ctx, e.logger, duty.Slot, consensusData, dummyValueChecker{}))

	for _, msg := range spectestingutils.SSVDecidingMsgsV(consensusData, e.keySet, ssvtypes.RoleAggregator) {
		require.NoError(t, e.runner.ProcessConsensus(ctx, e.logger, msg))
	}
}

// aggregatorPostConsensusMsgs returns the (valid) post-consensus PartialSignatureMessages from
// enough signers to cross quorum for the given duty/version.
func aggregatorPostConsensusMsgs(keySet *spectestingutils.TestKeySet, version spec.DataVersion) []*spectypes.PartialSignatureMessages {
	msgs := make([]*spectypes.PartialSignatureMessages, 0, keySet.Threshold)
	for i := uint64(1); i <= keySet.Threshold; i++ {
		msgs = append(msgs, spectestingutils.PostConsensusAggregatorMsg(keySet.Shares[i], i, version))
	}
	return msgs
}

// TestAggregatorRunnerProcessPostConsensus_MarksFailedOnSubmitError asserts that a beacon submit
// failure (a terminal post-quorum error) concludes the duty as failed for the legacy AggregatorRunner
// — the counterpart of the AggregatorCommitteeRunner and CommitteeRunner regressions already covered.
func TestAggregatorRunnerProcessPostConsensus_MarksFailedOnSubmitError(t *testing.T) {
	ctx := t.Context()
	const version = spec.DataVersionPhase0

	base := protocoltesting.NewTestingBeaconNodeWrapped().(*protocoltesting.BeaconNodeWrapped)
	submitErr := errors.New("beacon rejected aggregate")
	faulty := &faultyAggregateSubmitBeacon{BeaconNodeWrapped: base, submitErr: submitErr}

	env := newAggregatorRunnerEnv(t, faulty)
	duty := &spectypes.ValidatorDuty{
		Type:           spectypes.BNRoleAggregator,
		PubKey:         spectestingutils.TestingValidatorPubKey,
		Slot:           spectestingutils.TestingDutySlotV(version),
		ValidatorIndex: spectestingutils.TestingValidatorIndex,
	}

	env.startAndDecideAggregatorDuty(t, ctx, duty, version)

	concluded := make(chan dutyConclusion, 1)
	env.runner.dutyConcluded = concluded

	var postConsensusErr error
	for _, psig := range aggregatorPostConsensusMsgs(env.keySet, version) {
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

// TestAggregatorRunnerProcessPostConsensus_DoesNotMarkFailedOnInvalidSigs is the regression test for
// the legacy AggregatorRunner side of #2919: a recoverable reconstruct-invalid-signatures failure
// (wrapped in recoverableReconstructError at the push site in aggregator.go) must NOT conclude the
// duty failed, mirroring the CommitteeRunner fix from #2918/#2912.
func TestAggregatorRunnerProcessPostConsensus_DoesNotMarkFailedOnInvalidSigs(t *testing.T) {
	ctx := t.Context()
	const version = spec.DataVersionPhase0

	base := protocoltesting.NewTestingBeaconNodeWrapped().(*protocoltesting.BeaconNodeWrapped)
	env := newAggregatorRunnerEnv(t, base)
	duty := &spectypes.ValidatorDuty{
		Type:           spectypes.BNRoleAggregator,
		PubKey:         spectestingutils.TestingValidatorPubKey,
		Slot:           spectestingutils.TestingDutySlotV(version),
		ValidatorIndex: spectestingutils.TestingValidatorIndex,
	}

	env.startAndDecideAggregatorDuty(t, ctx, duty, version)

	concluded := make(chan dutyConclusion, 1)
	env.runner.dutyConcluded = concluded

	// Corrupt the beacon partial signatures. ValidatePostConsensusMsg only checks message structure
	// (not the beacon sig), so these still reach optimistic quorum, then ReconstructBeaconSig fails
	// and the defer must see the recoverableReconstructError tag and skip markDutyFailed.
	var postConsensusErr error
	for _, psig := range aggregatorPostConsensusMsgs(env.keySet, version) {
		for _, m := range psig.Messages {
			m.PartialSignature = bytes.Repeat([]byte{0xEE}, len(m.PartialSignature))
		}
		if err := env.runner.ProcessPostConsensus(ctx, env.logger, psig); err != nil {
			postConsensusErr = err
		}
	}

	require.Error(t, postConsensusErr, "invalid signatures should surface an error")
	require.True(t, isRecoverableReconstructError(postConsensusErr),
		"invalid-sigs must carry the recoverableReconstructError tag")

	require.Empty(t, concluded, "a recoverable invalid-sigs error must NOT conclude the duty")
	require.False(t, env.runner.State.Succeeded)
}
