package runner

import (
	"context"
	"errors"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
	"github.com/ssvlabs/ssv/protocol/v2/types/ssvtestingutils"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

func envelopeDuty(slot phase0.Slot) *spectypes.ValidatorDuty {
	return &spectypes.ValidatorDuty{
		Type:           spectypes.BNRoleEnvelopeProposer,
		PubKey:         spectestingutils.TestingValidatorPubKey,
		Slot:           slot,
		ValidatorIndex: spectestingutils.TestingValidatorIndex,
	}
}

// newEnvelopeTestBeacon embeds the spec testing beacon so DomainData (used by the post-consensus
// signing-root computation) resolves, while still recording the published envelopes.
func newEnvelopeTestBeacon() *envelopeTestBeacon {
	return &envelopeTestBeacon{BeaconNode: protocoltesting.NewTestingBeaconNodeWrapped()}
}

func newEnvelopeProposerRunnerForTest(t *testing.T, bn beacon.BeaconNode) (*EnvelopeProposerRunner, *spectestingutils.TestKeySet) {
	t.Helper()

	cfg := cloneTestNetworkConfig()
	keySet := spectestingutils.Testing4SharesSet()
	share := spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex)
	identifier := ssvtestingutils.NewMsgID(spectypes.JatoTestnet, spectestingutils.TestingValidatorPubKey[:], spectypes.RoleEnvelopeProposer)
	network := protocoltesting.NewTestingNetwork(1, keySet.OperatorKeys[1])
	km := ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager())
	operator := spectestingutils.TestingCommitteeMember(keySet)
	operatorSigner := spectestingutils.NewOperatorSigner(keySet, 1)

	qbftConfig := protocoltesting.TestingConfig(zap.NewNop(), keySet)
	qbftConfig.ProposerF = func(*specqbft.State, specqbft.Round) spectypes.OperatorID { return 1 }
	qbftConfig.Network = network
	controller := protocoltesting.NewTestingQBFTController(keySet, identifier[:], operator, qbftConfig, false)

	runnerIface, err := NewEnvelopeProposerRunner(EnvelopeProposerRunnerOptions{
		BaseRunnerOptions: BaseRunnerOptions{
			NetworkConfig:  cfg,
			Share:          map[phase0.ValidatorIndex]*spectypes.Share{share.ValidatorIndex: share},
			Beacon:         bn,
			Network:        network,
			Signer:         km,
			OperatorSigner: operatorSigner,
		},
		QBFTController:     controller,
		ProposedBlockRoots: ssv.NewProposedBlockRoots(),
	})
	require.NoError(t, err)

	r := runnerIface.(*EnvelopeProposerRunner)
	r.SetQBFTRoundTimerF(func(context.Context, *zap.Logger, phase0.Slot) ssv.QBFTRoundTimer {
		return roundtimer.NewTestingTimer()
	})
	return r, keySet
}

// setupEnvelopeRunnerForPostConsensus puts the runner in the decided state it would reach after §6 QBFT,
// so the post-consensus publish path can be exercised directly (mirrors setupRunnerForPostConsensus).
func setupEnvelopeRunnerForPostConsensus(t *testing.T, runner *EnvelopeProposerRunner, keySet *spectestingutils.TestKeySet, duty *spectypes.ValidatorDuty, cd *gloas.EnvelopeConsensusData) {
	t.Helper()

	runner.State = NewRunnerState(keySet.Threshold, duty)
	runner.measurements.StartDutyFlow()
	runner.measurements.StartConsensus()
	runner.measurements.EndConsensus()
	runner.measurements.StartPostConsensus()

	encoded, err := cd.Encode()
	require.NoError(t, err)
	runner.State.DecidedValue = encoded

	msgID := ssvtestingutils.NewMsgID(runner.NetworkConfig.DomainType, runner.GetShare().ValidatorPubKey[:], runner.RunnerRoleType)
	qbftConfig := protocoltesting.TestingConfig(zap.NewNop(), keySet)
	qbftConfig.ProposerF = func(*specqbft.State, specqbft.Round) spectypes.OperatorID { return 1 }
	qbftConfig.Network = runner.network
	runner.State.RunningInstance = instance.NewInstance(
		t.Context(), zap.NewNop(), qbftConfig, spectestingutils.TestingCommitteeMember(keySet),
		msgID[:], specqbft.Height(duty.Slot), runner.operatorSigner,
		func(context.Context, *zap.Logger, phase0.Slot) ssv.QBFTRoundTimer {
			return roundtimer.NewTestingTimer()
		},
	)
	runner.State.RunningInstance.State.Decided = true
	runner.State.RunningInstance.State.DecidedValue = encoded
}

func decidedEnvelopeConsensusData(t *testing.T, slot phase0.Slot, envelope *gloas.ExecutionPayloadEnvelope) *gloas.EnvelopeConsensusData {
	t.Helper()
	blinded, err := gloas.Blinded(envelope)
	require.NoError(t, err)
	dataSSZ, err := blinded.Encode()
	require.NoError(t, err)
	return &gloas.EnvelopeConsensusData{Duty: *envelopeDuty(slot), DataSSZ: dataSSZ}
}

// The builder produces the envelope, caches it for the later content-matched publish, and — leading
// round 1 in this harness — proposes its blinded form.
func TestEnvelopeProposerRunner_ExecuteDutyProposesWhenProduceSucceeds(t *testing.T) {
	const slot = phase0.Slot(8)
	bn := newEnvelopeTestBeacon()
	bn.envelope = sampleEnvelope() // commits to block root 0xaa
	runner, _ := newEnvelopeProposerRunnerForTest(t, bn)
	runner.proposedBlockRoots.Set(slot, phase0.Root{0xaa})

	require.NoError(t, runner.StartNewDuty(context.Background(), zap.NewNop(), envelopeDuty(slot), 3))

	require.NotNil(t, runner.State.RunningInstance)
	require.Same(t, bn.envelope, runner.cachedEnvelope)

	broadcast := runner.network.(*protocoltesting.TestingNetwork).BroadcastedMsgs
	require.Len(t, broadcast, 1)
	proposal, err := specqbft.DecodeMessage(broadcast[0].SSVMessage.Data)
	require.NoError(t, err)
	require.Equal(t, specqbft.ProposalMsgType, proposal.MsgType)
	require.Equal(t, specqbft.Height(slot), proposal.Height)
}

// An operator whose beacon node cannot produce the envelope — every non-builder on a cluster whose
// operators run separate beacon nodes — still joins the §6 round: the QBFT instance starts as a voter
// with no value of its own, and, leading round 1 in this harness, it broadcasts no proposal rather than
// an empty one. With nothing cached it will not publish either.
func TestEnvelopeProposerRunner_ExecuteDutyJoinsAsVoterWhenProduceFails(t *testing.T) {
	const slot = phase0.Slot(8)
	bn := newEnvelopeTestBeacon()
	bn.produceErr = errors.New("404 execution payload envelope not found")
	runner, _ := newEnvelopeProposerRunnerForTest(t, bn)
	runner.proposedBlockRoots.Set(slot, phase0.Root{0xaa})

	require.NoError(t, runner.StartNewDuty(context.Background(), zap.NewNop(), envelopeDuty(slot), 3))

	require.True(t, runner.HasRunningDuty())
	require.NotNil(t, runner.State.RunningInstance, "a voter must hold a running QBFT instance to vote in")
	require.Empty(t, runner.State.RunningInstance.StartValue)
	require.Nil(t, runner.cachedEnvelope)
	require.Empty(t, runner.network.(*protocoltesting.TestingNetwork).BroadcastedMsgs)
}

// The builder — its cached envelope blinds to the decided value — publishes the full signed envelope.
func TestEnvelopeProposerRunner_SubmitEnvelopeProposerPublishes(t *testing.T) {
	const slot = phase0.Slot(8)
	envelope := sampleEnvelope()
	cd := decidedEnvelopeConsensusData(t, slot, envelope)

	bn := newEnvelopeTestBeacon()
	runner, keySet := newEnvelopeProposerRunnerForTest(t, bn)
	setupEnvelopeRunnerForPostConsensus(t, runner, keySet, envelopeDuty(slot), cd)
	runner.cachedEnvelope = envelope // this operator built the decided envelope

	err := runner.submitEnvelope(context.Background(), zap.NewNop(), cd, phase0.BLSSignature{0xab})
	require.NoError(t, err)

	require.Len(t, bn.submitted, 1)
	require.Equal(t, envelope, bn.submitted[0].Message)
	require.Equal(t, phase0.BLSSignature{0xab}, bn.submitted[0].Signature)
	require.True(t, runner.State.Succeeded)
}

// An operator that produced a competing envelope (content mismatch) completes the duty without publishing —
// only the builder of the decided envelope holds the matching full bytes.
func TestEnvelopeProposerRunner_SubmitEnvelopeNonBuilderSkips(t *testing.T) {
	const slot = phase0.Slot(8)
	cd := decidedEnvelopeConsensusData(t, slot, sampleEnvelope())

	bn := newEnvelopeTestBeacon()
	runner, keySet := newEnvelopeProposerRunnerForTest(t, bn)
	setupEnvelopeRunnerForPostConsensus(t, runner, keySet, envelopeDuty(slot), cd)

	// This operator's cached envelope differs from the decided one (it lost the round), so it does not
	// blind to the decided value.
	competing := sampleEnvelope()
	competing.Payload.BlockNumber = 99
	runner.cachedEnvelope = competing

	err := runner.submitEnvelope(context.Background(), zap.NewNop(), cd, phase0.BLSSignature{0xab})
	require.NoError(t, err)

	require.Empty(t, bn.submitted)
	require.True(t, runner.State.Succeeded)
}

// processEnvelopePostConsensusQuorum feeds a threshold of post-consensus partial signatures over the decided
// blinded envelope root (signed under DOMAIN_BEACON_BUILDER with the share keys), driving the runner to
// reconstruct the envelope signature. Mirrors processPostConsensusQuorum — the envelope role has no spec
// message helper, so the partial signatures are built here.
func processEnvelopePostConsensusQuorum(t *testing.T, runner *EnvelopeProposerRunner, keySet *spectestingutils.TestKeySet, blinded *gloas.BlindedExecutionPayloadEnvelope, slot phase0.Slot) {
	t.Helper()

	signer := spectestingutils.NewTestingKeyManager()
	// epoch doesn't matter here — the testing beacon's domain is epoch-invariant, so it matches the
	// domain the runner derives at the duty's epoch.
	domain, err := spectestingutils.NewTestingBeaconNode().DomainData(1, spectypes.DomainBeaconBuilder)
	require.NoError(t, err)

	root, err := blinded.HashTreeRoot()
	require.NoError(t, err)
	hashRoot := spectypes.SSZ32Bytes(root)

	for opID := spectypes.OperatorID(1); opID <= keySet.Threshold; opID++ {
		sig, sr, err := signer.SignBeaconObject(hashRoot, domain, keySet.Shares[opID].GetPublicKey().Serialize(), spectypes.DomainBeaconBuilder)
		require.NoError(t, err)
		blsSig := phase0.BLSSignature{}
		copy(blsSig[:], sig)

		require.NoError(t, runner.ProcessPostConsensus(context.Background(), zap.NewNop(), &spectypes.PartialSignatureMessages{
			Type: spectypes.PostConsensusPartialSig,
			Slot: slot,
			Messages: []*spectypes.PartialSignatureMessage{{
				PartialSignature: blsSig[:],
				SigningRoot:      sr,
				Signer:           opID,
				ValidatorIndex:   spectestingutils.TestingValidatorIndex,
			}},
		}))
	}
}

// ProcessPostConsensus collects a quorum of partial signatures, reconstructs the BLS signature, and (as the
// builder) publishes the full signed envelope carrying it.
func TestEnvelopeProposerRunner_ProcessPostConsensusReconstructsAndPublishes(t *testing.T) {
	const slot = phase0.Slot(8)
	envelope := sampleEnvelope()
	cd := decidedEnvelopeConsensusData(t, slot, envelope)

	bn := newEnvelopeTestBeacon()
	runner, keySet := newEnvelopeProposerRunnerForTest(t, bn)
	setupEnvelopeRunnerForPostConsensus(t, runner, keySet, envelopeDuty(slot), cd)
	runner.cachedEnvelope = envelope // this operator built the decided envelope

	blinded, err := gloas.Blinded(envelope)
	require.NoError(t, err)
	processEnvelopePostConsensusQuorum(t, runner, keySet, blinded, slot)

	require.Len(t, bn.submitted, 1)
	require.Equal(t, envelope, bn.submitted[0].Message)
	require.NotEqual(t, phase0.BLSSignature{}, bn.submitted[0].Signature) // the reconstructed signature
	require.True(t, runner.State.Succeeded)
}
