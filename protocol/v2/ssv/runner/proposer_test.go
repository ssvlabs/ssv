package runner

import (
	"context"
	"errors"
	"maps"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	blindutil "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon/blind"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
	"github.com/ssvlabs/ssv/protocol/v2/types/ssvtestingutils"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

type proposerTestBeacon struct {
	beacon.BeaconNode

	getProposal *api.VersionedProposal

	getCalls        int
	lastGetSlot     phase0.Slot
	lastGetGraffiti []byte
	lastGetRandao   []byte
	submittedBlocks []*api.VersionedProposal
	submittedSig    []phase0.BLSSignature
	submitErr       error

	getGloasBlock        *gloas.BeaconBlock
	getGloasBuilderURL   string // the Eth-Builder-Url the produce returns; empty = self-build / p2p win
	submittedGloasBlocks []*gloas.SignedBeaconBlock
}

func newProposerTestBeacon(proposal *api.VersionedProposal) *proposerTestBeacon {
	return &proposerTestBeacon{
		BeaconNode:  protocoltesting.NewTestingBeaconNodeWrapped(),
		getProposal: proposal,
	}
}

func (b *proposerTestBeacon) GetBeaconBlock(_ context.Context, slot phase0.Slot, graffiti, randao []byte) (*api.VersionedProposal, ssz.Marshaler, error) {
	b.getCalls++
	b.lastGetSlot = slot
	b.lastGetGraffiti = append([]byte(nil), graffiti...)
	b.lastGetRandao = append([]byte(nil), randao...)
	return b.getProposal, nil, nil
}

func (b *proposerTestBeacon) SubmitBeaconBlock(_ context.Context, block *api.VersionedProposal, sig phase0.BLSSignature) error {
	b.submittedBlocks = append(b.submittedBlocks, block)
	b.submittedSig = append(b.submittedSig, sig)
	return b.submitErr
}

func (b *proposerTestBeacon) GetGloasBeaconBlock(_ context.Context, slot phase0.Slot, graffiti, randao []byte, _ *gloas.ProduceBuilderConfig) (*gloas.BeaconBlock, string, error) {
	b.getCalls++
	b.lastGetSlot = slot
	b.lastGetGraffiti = append([]byte(nil), graffiti...)
	b.lastGetRandao = append([]byte(nil), randao...)
	return b.getGloasBlock, b.getGloasBuilderURL, nil
}

func (b *proposerTestBeacon) SubmitGloasBeaconBlock(_ context.Context, block *gloas.SignedBeaconBlock, _ string) error {
	b.submittedGloasBlocks = append(b.submittedGloasBlocks, block)
	return b.submitErr
}

// decidedBuilderURL echoes this operator's produce Eth-Builder-Url on publish only when the decided block
// is the one this operator produced (owner-match) and a builder bid actually won.
func TestProposerRunner_decidedBuilderURL(t *testing.T) {
	block := gloas.TestingBeaconBlock(7)
	root, err := block.HashTreeRoot()
	require.NoError(t, err)

	// This operator produced the decided block and its BN returned a builder URL -> echo it.
	owner := &ProposerRunner{gloasBuilderURL: "https://b.example", gloasProducedRoot: root}
	require.Equal(t, "https://b.example", owner.decidedBuilderURL(block))

	// Another operator's block won QBFT (root mismatch) -> no echo; this BN never solicited that bid.
	mismatch := &ProposerRunner{gloasBuilderURL: "https://b.example", gloasProducedRoot: [32]byte{0xff}}
	require.Empty(t, mismatch.decidedBuilderURL(block))

	// Self-build / p2p win (no builder URL) -> no echo even on an owner match.
	noURL := &ProposerRunner{gloasBuilderURL: "", gloasProducedRoot: root}
	require.Empty(t, noURL.decidedBuilderURL(block))
}

type stubDoppelganger struct {
	canSign      bool
	reportQuorum []phase0.ValidatorIndex
}

func (d *stubDoppelganger) CanSign(phase0.ValidatorIndex) bool {
	return d.canSign
}

func (d *stubDoppelganger) ReportQuorum(validatorIndex phase0.ValidatorIndex) {
	d.reportQuorum = append(d.reportQuorum, validatorIndex)
}

type fixedOperatorSigner struct {
	id spectypes.OperatorID
}

func (s fixedOperatorSigner) SignSSVMessage(*spectypes.SSVMessage) ([]byte, error) {
	return []byte("test-signature"), nil
}

func (s fixedOperatorSigner) GetOperatorID() spectypes.OperatorID { return s.id }

func TestProposerRunnerProcessPreConsensusCachesFullBlockAndFetchesWithReconstructedRandao(t *testing.T) {
	t.Parallel()

	version := spec.DataVersionDeneb
	duty := spectestingutils.TestingProposerDutyV(version)
	fullBlock := spectestingutils.TestingBeaconBlockV(version)
	beacon := newProposerTestBeacon(fullBlock)
	runner, keySet, _ := newProposerRunnerForTest(t, beacon, &stubDoppelganger{canSign: true}, 0, nil)

	err := runner.StartNewDuty(context.Background(), zap.NewNop(), duty, keySet.Threshold)
	require.NoError(t, err)

	var expectedRoot [32]byte
	ctx := context.Background()
	logger := zap.NewNop()
	for operatorID := spectypes.OperatorID(1); operatorID <= keySet.Threshold; operatorID++ {
		msg := spectestingutils.PreConsensusRandaoMsgV(keySet.Shares[operatorID], operatorID, version)
		if operatorID == 1 {
			expectedRoot = msg.Messages[0].SigningRoot
		} else {
			require.Equal(t, expectedRoot, msg.Messages[0].SigningRoot)
		}
		require.NoError(t, runner.ProcessPreConsensus(ctx, logger, msg))
	}

	expectedRandao, err := runner.State.ReconstructBeaconSig(
		runner.State.PreConsensusContainer,
		expectedRoot,
		runner.GetShare().ValidatorPubKey[:],
		runner.GetShare().ValidatorIndex,
	)
	require.NoError(t, err)

	_, blindedMarshaler, err := blindutil.EnsureBlinded(fullBlock)
	require.NoError(t, err)
	expectedBlindedSSZ, err := blindedMarshaler.MarshalSSZ()
	require.NoError(t, err)

	require.Equal(t, 1, beacon.getCalls)
	require.Equal(t, duty.Slot, beacon.lastGetSlot)
	require.Equal(t, []byte("graffiti"), beacon.lastGetGraffiti)
	require.Equal(t, expectedRandao, beacon.lastGetRandao)
	require.Same(t, fullBlock, runner.cachedFullBlock)
	require.Equal(t, expectedBlindedSSZ, runner.cachedBlindedBlockSSZ)
	require.NotNil(t, runner.State.RunningInstance)
}

func TestProposerRunnerProcessPreConsensusDoesNotCacheBlindedBlock(t *testing.T) {
	t.Parallel()

	version := spec.DataVersionDeneb
	duty := spectestingutils.TestingProposerDutyV(version)
	blindedBlock := spectestingutils.TestingBlindedBeaconBlockV(version)
	beacon := newProposerTestBeacon(blindedBlock)
	runner, keySet, _ := newProposerRunnerForTest(t, beacon, &stubDoppelganger{canSign: true}, 0, nil)

	err := runner.StartNewDuty(context.Background(), zap.NewNop(), duty, keySet.Threshold)
	require.NoError(t, err)

	processPreConsensusQuorum(t, runner, keySet, version)

	require.Equal(t, 1, beacon.getCalls)
	require.Nil(t, runner.cachedFullBlock)
	require.Nil(t, runner.cachedBlindedBlockSSZ)
}

func TestProposerRunnerProcessPreConsensusReturnsContextCanceledDuringProposerDelay(t *testing.T) {
	t.Parallel()

	version := spec.DataVersionDeneb
	duty := spectestingutils.TestingProposerDutyV(version)
	cfg := cloneTestNetworkConfig()
	cfg.GenesisTime = time.Now().Add(-time.Duration(duty.Slot)*cfg.SlotDuration + time.Second)

	beacon := newProposerTestBeacon(spectestingutils.TestingBeaconBlockV(version))
	runner, keySet, _ := newProposerRunnerForTest(t, beacon, &stubDoppelganger{canSign: true}, 3*time.Second, cfg)

	err := runner.StartNewDuty(context.Background(), zap.NewNop(), duty, keySet.Threshold)
	require.NoError(t, err)

	logger := zap.NewNop()
	for operatorID := spectypes.OperatorID(1); operatorID < keySet.Threshold; operatorID++ {
		msg := spectestingutils.PreConsensusRandaoMsgV(keySet.Shares[operatorID], operatorID, version)
		require.NoError(t, runner.ProcessPreConsensus(context.Background(), logger, msg))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	finalMsg := spectestingutils.PreConsensusRandaoMsgV(keySet.Shares[keySet.Threshold], keySet.Threshold, version)
	err = runner.ProcessPreConsensus(ctx, logger, finalMsg)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 0, beacon.getCalls)
	require.Nil(t, runner.cachedFullBlock)
	require.Nil(t, runner.State.RunningInstance)
}

func TestRemainingProposerDelay(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	slot := phase0.Slot(7)

	tests := []struct {
		name          string
		slotTime      time.Time
		proposerDelay time.Duration
		now           time.Time
		want          time.Duration
	}{
		{
			name:          "waits remaining delay when slot already started",
			slotTime:      now.Add(-30 * time.Millisecond),
			proposerDelay: 80 * time.Millisecond,
			now:           now,
			want:          50 * time.Millisecond,
		},
		{
			name:          "returns zero when already past proposer delay",
			slotTime:      now.Add(-120 * time.Millisecond),
			proposerDelay: 80 * time.Millisecond,
			now:           now,
			want:          0,
		},
		{
			name:          "handles future slot start",
			slotTime:      now.Add(20 * time.Millisecond),
			proposerDelay: 80 * time.Millisecond,
			now:           now,
			want:          100 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := cloneTestNetworkConfig()
			cfg.GenesisTime = tt.slotTime.Add(-time.Duration(slot) * cfg.SlotDuration)
			runner := &ProposerRunner{
				BaseRunner:    &BaseRunner{NetworkConfig: cfg},
				proposerDelay: tt.proposerDelay,
			}

			require.Equal(t, tt.want, runner.remainingProposerDelay(slot, tt.now))
		})
	}
}

// proposerDelayForSlot is fork-gated: pre-Gloas uses ProposerDelay, Gloas-on uses ProposerDelayEPBS.
func TestProposerDelayForSlot(t *testing.T) {
	const gloasEpoch = 5
	netCfg := networkconfig.TestNetworkWithGloas(gloasEpoch)
	r := &ProposerRunner{
		BaseRunner:        &BaseRunner{NetworkConfig: netCfg},
		proposerDelay:     300 * time.Millisecond,
		proposerDelayEPBS: 100 * time.Millisecond,
	}

	preGloasSlot := phase0.Slot(uint64(gloasEpoch-1) * netCfg.SlotsPerEpoch)
	gloasSlot := phase0.Slot(uint64(gloasEpoch) * netCfg.SlotsPerEpoch)

	require.Equal(t, 300*time.Millisecond, r.proposerDelayForSlot(preGloasSlot))
	require.Equal(t, 100*time.Millisecond, r.proposerDelayForSlot(gloasSlot))
}

func TestProposerRunnerStartNewDutySkipsRandaoSigningWhenDoppelgangerBlocks(t *testing.T) {
	t.Parallel()

	version := spec.DataVersionDeneb
	duty := spectestingutils.TestingProposerDutyV(version)
	dg := &stubDoppelganger{canSign: false}
	beacon := newProposerTestBeacon(spectestingutils.TestingBeaconBlockV(version))
	runner, _, network := newProposerRunnerForTest(t, beacon, dg, 0, nil)

	err := runner.StartNewDuty(context.Background(), zap.NewNop(), duty, 3)
	require.NoError(t, err)

	require.Equal(t, 0, countPartialSignatureBroadcastsByType(t, network, spectypes.RandaoPartialSig))
	require.Equal(t, 0, beacon.getCalls)
	require.Nil(t, runner.cachedFullBlock)
	require.Nil(t, runner.State.RunningInstance)
	require.False(t, runner.State.Succeeded)
	require.Empty(t, dg.reportQuorum)
}

func TestProposerRunnerProcessConsensusSkipsPostConsensusSigningWhenDoppelgangerBlocks(t *testing.T) {
	t.Parallel()

	version := spec.DataVersionDeneb
	duty := spectestingutils.TestingProposerDutyV(version)
	dg := &stubDoppelganger{canSign: true}
	beacon := newProposerTestBeacon(spectestingutils.TestingBeaconBlockV(version))
	runner, keySet, network := newProposerRunnerForTest(t, beacon, dg, 0, nil)

	err := runner.StartNewDuty(context.Background(), zap.NewNop(), duty, keySet.Threshold)
	require.NoError(t, err)

	dg.canSign = false

	consensusData := spectestingutils.TestProposerBlindedBlockConsensusDataV(version)
	runner.measurements.StartConsensus()
	require.NoError(t, runner.decide(context.Background(), zap.NewNop(), duty.Slot, consensusData, runner.ValCheck))
	consensusMsgs := spectestingutils.SSVDecidingMsgsForHeight(
		consensusData,
		runner.QBFTController.GetIdentifier(),
		specqbft.Height(consensusData.Duty.Slot),
		keySet,
	)

	for _, msg := range consensusMsgs {
		require.NoError(t, runner.ProcessConsensus(context.Background(), zap.NewNop(), msg))
	}

	require.NotNil(t, runner.State.DecidedValue)
	require.Equal(t, 0, countPartialSignatureBroadcastsByType(t, network, spectypes.PostConsensusPartialSig))
	require.Equal(t, 1, countPartialSignatureBroadcastsByType(t, network, spectypes.RandaoPartialSig))
	require.False(t, runner.State.Succeeded)
}

func TestProposerRunnerProcessPostConsensusLeaderUsesCachedFullBlockWhenDecisionMatches(t *testing.T) {
	t.Parallel()

	version := spec.DataVersionDeneb
	fullBlock := spectestingutils.TestingBeaconBlockV(version)
	consensusData := spectestingutils.TestProposerBlindedBlockConsensusDataV(version)
	beacon := newProposerTestBeacon(nil)
	dg := &stubDoppelganger{canSign: true}
	runner, keySet, _ := newProposerRunnerForTest(t, beacon, dg, 0, nil)

	setupRunnerForPostConsensus(t, runner, keySet, spectestingutils.TestingProposerDutyV(version), consensusData, 1)
	runner.cachedFullBlock = fullBlock
	runner.cachedBlindedBlockSSZ = append([]byte(nil), consensusData.DataSSZ...)

	processPostConsensusQuorum(t, runner, keySet, version)

	require.Len(t, beacon.submittedBlocks, 1)
	require.Same(t, fullBlock, beacon.submittedBlocks[0])
	require.False(t, beacon.submittedBlocks[0].Blinded)
	require.NotEqual(t, phase0.BLSSignature{}, beacon.submittedSig[0])
	require.Equal(t, []phase0.ValidatorIndex{runner.GetShare().ValidatorIndex}, dg.reportQuorum)
	require.True(t, runner.State.Succeeded)
}

func TestProposerRunnerProcessPostConsensusLeaderFallsBackToDecidedBlindedBlockOnCacheMismatch(t *testing.T) {
	t.Parallel()

	version := spec.DataVersionDeneb
	consensusData := spectestingutils.TestProposerBlindedBlockConsensusDataV(version)
	beacon := newProposerTestBeacon(nil)
	dg := &stubDoppelganger{canSign: true}
	runner, keySet, _ := newProposerRunnerForTest(t, beacon, dg, 0, nil)

	setupRunnerForPostConsensus(t, runner, keySet, spectestingutils.TestingProposerDutyV(version), consensusData, 1)
	runner.cachedFullBlock = spectestingutils.TestingBeaconBlockV(version)
	runner.cachedBlindedBlockSSZ = []byte("different-blinded-block")

	processPostConsensusQuorum(t, runner, keySet, version)

	require.Len(t, beacon.submittedBlocks, 1)
	require.True(t, beacon.submittedBlocks[0].Blinded)
	require.Equal(t, []phase0.ValidatorIndex{runner.GetShare().ValidatorIndex}, dg.reportQuorum)
	require.True(t, runner.State.Succeeded)
}

func TestProposerRunnerProcessPostConsensusNonLeaderKeepsDecidedBlindedBlock(t *testing.T) {
	t.Parallel()

	version := spec.DataVersionDeneb
	consensusData := spectestingutils.TestProposerBlindedBlockConsensusDataV(version)
	beacon := newProposerTestBeacon(nil)
	dg := &stubDoppelganger{canSign: true}
	runner, keySet, _ := newProposerRunnerForTest(t, beacon, dg, 0, nil)

	setupRunnerForPostConsensus(t, runner, keySet, spectestingutils.TestingProposerDutyV(version), consensusData, 1)
	runner.operatorSigner = fixedOperatorSigner{id: 2}
	runner.cachedFullBlock = spectestingutils.TestingBeaconBlockV(version)
	runner.cachedBlindedBlockSSZ = append([]byte(nil), consensusData.DataSSZ...)

	processPostConsensusQuorum(t, runner, keySet, version)

	require.Len(t, beacon.submittedBlocks, 1)
	require.True(t, beacon.submittedBlocks[0].Blinded)
	require.Equal(t, []phase0.ValidatorIndex{runner.GetShare().ValidatorIndex}, dg.reportQuorum)
	require.True(t, runner.State.Succeeded)
}

func gloasProposerDuty(slot phase0.Slot) *spectypes.ValidatorDuty {
	return &spectypes.ValidatorDuty{
		Type:                    spectypes.BNRoleProposer,
		PubKey:                  spectestingutils.TestingValidatorPubKey,
		Slot:                    slot,
		ValidatorIndex:          spectestingutils.TestingValidatorIndex,
		CommitteeIndex:          3,
		CommitteesAtSlot:        36,
		CommitteeLength:         128,
		ValidatorCommitteeIndex: 11,
	}
}

func gloasProposerConsensusData(t *testing.T, slot phase0.Slot) *spectypes.ProposerConsensusData {
	t.Helper()
	dataSSZ, err := gloas.TestingBeaconBlock(slot).MarshalSSZ()
	require.NoError(t, err)
	return &spectypes.ProposerConsensusData{
		Duty:    *gloasProposerDuty(slot),
		Version: networkconfig.DataVersionGloas,
		DataSSZ: dataSSZ,
	}
}

// Every operator submits the decided Gloas block (it is bid-only, so all hold it and submission is
// idempotent at the BN), then completes the duty.
func TestProposerRunnerSubmitGloasProposalSubmits(t *testing.T) {
	t.Parallel()

	const slot = phase0.Slot(8)
	consensusData := gloasProposerConsensusData(t, slot)
	beacon := newProposerTestBeacon(nil)
	runner, keySet, _ := newProposerRunnerForTest(t, beacon, &stubDoppelganger{canSign: true}, 0, nil)

	setupRunnerForPostConsensus(t, runner, keySet, gloasProposerDuty(slot), consensusData, 1)

	err := runner.submitGloasProposal(context.Background(), zap.NewNop(), trace.SpanFromContext(context.Background()), consensusData, phase0.BLSSignature{0xab})
	require.NoError(t, err)

	require.Len(t, beacon.submittedGloasBlocks, 1)
	require.Equal(t, slot, beacon.submittedGloasBlocks[0].Message.Slot)
	require.Equal(t, phase0.BLSSignature{0xab}, beacon.submittedGloasBlocks[0].Signature)
	require.True(t, runner.State.Succeeded)
}

// gloasProposalInput fetches the Gloas block from the beacon node and wraps it as the consensus value
// with the Gloas version marker.
func TestProposerRunnerGloasProposalInput(t *testing.T) {
	t.Parallel()

	const slot = phase0.Slot(8)
	beacon := newProposerTestBeacon(nil)
	beacon.getGloasBlock = gloas.TestingBeaconBlock(slot)
	beacon.getGloasBuilderURL = "https://b.example"
	runner, _, _ := newProposerRunnerForTest(t, beacon, &stubDoppelganger{canSign: true}, 0, nil)

	input, err := runner.gloasProposalInput(context.Background(), zap.NewNop(), gloasProposerDuty(slot), []byte("randao"))
	require.NoError(t, err)

	expectedSSZ, err := gloas.TestingBeaconBlock(slot).MarshalSSZ()
	require.NoError(t, err)
	require.Equal(t, networkconfig.DataVersionGloas, input.Version)
	require.Equal(t, expectedSSZ, input.DataSSZ)
	require.Equal(t, slot, beacon.lastGetSlot)
	require.Equal(t, []byte("graffiti"), beacon.lastGetGraffiti)
	require.Equal(t, []byte("randao"), beacon.lastGetRandao)

	// The produce output is recorded for the publish-time owner-match (see decidedBuilderURL).
	expectedRoot, err := gloas.TestingBeaconBlock(slot).HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, expectedRoot, runner.gloasProducedRoot)
	require.Equal(t, "https://b.example", runner.gloasBuilderURL)
}

// StartNewDuty clears the previous duty's Gloas produce markers along with the cached pre-Gloas block, so a
// stale owner-match can't echo an old Eth-Builder-Url on the next proposal.
func TestProposerRunnerStartNewDutyResetsGloasProduceMarkers(t *testing.T) {
	t.Parallel()

	version := spec.DataVersionDeneb
	beacon := newProposerTestBeacon(spectestingutils.TestingBeaconBlockV(version))
	runner, _, _ := newProposerRunnerForTest(t, beacon, &stubDoppelganger{canSign: true}, 0, nil)
	runner.gloasProducedRoot = [32]byte{0xaa}
	runner.gloasBuilderURL = "https://stale.example"

	require.NoError(t, runner.StartNewDuty(context.Background(), zap.NewNop(), spectestingutils.TestingProposerDutyV(version), 3))

	require.Equal(t, [32]byte{}, runner.gloasProducedRoot)
	require.Empty(t, runner.gloasBuilderURL)
}

func newProposerRunnerForTest(
	t *testing.T,
	beacon *proposerTestBeacon,
	dg *stubDoppelganger,
	proposerDelay time.Duration,
	cfg *networkconfig.Network,
) (*ProposerRunner, *spectestingutils.TestKeySet, *protocoltesting.TestingNetwork) {
	t.Helper()

	if cfg == nil {
		cfg = cloneTestNetworkConfig()
	}

	logger := zap.NewNop()
	keySet := spectestingutils.Testing4SharesSet()
	share := spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex)
	identifier := ssvtestingutils.NewMsgID(spectypes.JatoTestnet, spectestingutils.TestingValidatorPubKey[:], spectypes.RoleProposer)
	network := protocoltesting.NewTestingNetwork(1, keySet.OperatorKeys[1])
	km := ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager())
	operator := spectestingutils.TestingCommitteeMember(keySet)
	operatorSigner := spectestingutils.NewOperatorSigner(keySet, 1)
	valCheck := ssv.NewProposerChecker(
		km,
		cfg.Beacon,
		spectypes.ValidatorPK(spectestingutils.TestingValidatorPubKey),
		spectestingutils.TestingValidatorIndex,
		phase0.BLSPubKey(share.SharePubKey),
	)

	qbftConfig := protocoltesting.TestingConfig(logger, keySet)
	qbftConfig.ProposerF = func(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
		return 1
	}
	qbftConfig.Network = network
	controller := protocoltesting.NewTestingQBFTController(
		keySet,
		identifier[:],
		operator,
		qbftConfig,
		false,
	)

	shareMap := map[phase0.ValidatorIndex]*spectypes.Share{
		share.ValidatorIndex: share,
	}

	runnerIface, err := NewProposerRunner(ProposerRunnerOptions{
		BaseRunnerOptions: BaseRunnerOptions{
			NetworkConfig:  cfg,
			Share:          shareMap,
			Beacon:         beacon,
			Network:        network,
			Signer:         km,
			OperatorSigner: operatorSigner,
		},
		QBFTController:      controller,
		DoppelgangerHandler: dg,
		ValCheck:            valCheck,
		HighestDecidedSlot:  0,
		Graffiti:            []byte("graffiti"),
		ProposerDelay:       proposerDelay,
	})
	require.NoError(t, err)

	proposerRunner := runnerIface.(*ProposerRunner)
	proposerRunner.SetQBFTRoundTimerF(func(_ context.Context, _ *zap.Logger, _ phase0.Slot) ssv.QBFTRoundTimer {
		return roundtimer.NewTestingTimer()
	})
	return proposerRunner, keySet, network
}

func setupRunnerForPostConsensus(
	t *testing.T,
	runner *ProposerRunner,
	keySet *spectestingutils.TestKeySet,
	duty *spectypes.ValidatorDuty,
	consensusData *spectypes.ProposerConsensusData,
	leaderID spectypes.OperatorID,
) {
	t.Helper()

	runner.State = NewRunnerState(keySet.Threshold, duty)
	runner.measurements.StartDutyFlow()
	runner.measurements.StartConsensus()
	runner.measurements.EndConsensus()
	runner.measurements.StartPostConsensus()

	encodedDecidedValue, err := consensusData.Encode()
	require.NoError(t, err)
	runner.State.DecidedValue = encodedDecidedValue

	msgID := ssvtestingutils.NewMsgID(runner.NetworkConfig.DomainType, runner.GetShare().ValidatorPubKey[:], runner.RunnerRoleType)
	qbftConfig := protocoltesting.TestingConfig(zap.NewNop(), keySet)
	qbftConfig.ProposerF = func(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
		return leaderID
	}
	qbftConfig.Network = runner.network
	runner.State.RunningInstance = instance.NewInstance(
		t.Context(),
		zap.NewNop(),
		qbftConfig,
		spectestingutils.TestingCommitteeMember(keySet),
		msgID[:],
		specqbft.Height(duty.Slot),
		runner.operatorSigner,
		func(ctx context.Context, logger *zap.Logger, slot phase0.Slot) ssv.QBFTRoundTimer {
			return roundtimer.NewTestingTimer()
		},
	)
	runner.State.RunningInstance.State.Decided = true
	runner.State.RunningInstance.State.DecidedValue = encodedDecidedValue
}

func processPreConsensusQuorum(t *testing.T, runner *ProposerRunner, keySet *spectestingutils.TestKeySet, version spec.DataVersion) {
	t.Helper()

	ctx := context.Background()
	logger := zap.NewNop()
	for operatorID := spectypes.OperatorID(1); operatorID <= keySet.Threshold; operatorID++ {
		msg := spectestingutils.PreConsensusRandaoMsgV(keySet.Shares[operatorID], operatorID, version)
		require.NoError(t, runner.ProcessPreConsensus(ctx, logger, msg))
	}
}

func processPostConsensusQuorum(t *testing.T, runner *ProposerRunner, keySet *spectestingutils.TestKeySet, version spec.DataVersion) {
	t.Helper()

	ctx := context.Background()
	logger := zap.NewNop()
	for operatorID := spectypes.OperatorID(1); operatorID <= keySet.Threshold; operatorID++ {
		msg := spectestingutils.PostConsensusProposerMsgV(keySet.Shares[operatorID], operatorID, version)
		require.NoError(t, runner.ProcessPostConsensus(ctx, logger, msg))
	}
}

func countPartialSignatureBroadcastsByType(
	t *testing.T,
	network *protocoltesting.TestingNetwork,
	msgType spectypes.PartialSigMsgType,
) int {
	t.Helper()

	count := 0
	for _, msg := range network.BroadcastedMsgs {
		if msg.SSVMessage == nil || msg.SSVMessage.MsgType != spectypes.SSVPartialSignatureMsgType {
			continue
		}

		partialSigMsg := &spectypes.PartialSignatureMessages{}
		require.NoError(t, partialSigMsg.Decode(msg.SSVMessage.Data))
		if partialSigMsg.Type == msgType {
			count++
		}
	}

	return count
}

func cloneTestNetworkConfig() *networkconfig.Network {
	cfg := *networkconfig.TestNetwork
	beaconCfg := *networkconfig.TestNetwork.Beacon
	// Tests only mutate beacon timing fields; the rest of TestNetwork can remain shared.
	if beaconCfg.Forks != nil {
		beaconCfg.Forks = maps.Clone(beaconCfg.Forks)
	}
	cfg.Beacon = &beaconCfg
	// Clone SSV so tests can mutate Forks.Boole (or other SSV fields) without writing
	// through to the package-level TestNetwork global. SSVForks is a value type, so a
	// shallow *SSV copy fully isolates it.
	ssvCfg := *networkconfig.TestNetwork.SSV
	cfg.SSV = &ssvCfg
	return &cfg
}

func gloasExternalBuildConsensusData(t *testing.T, slot phase0.Slot) *spectypes.ProposerConsensusData {
	t.Helper()
	block := gloas.TestingBeaconBlock(slot)
	block.Body.SignedExecutionPayloadBid.Message.BuilderIndex = 5 // an external builder, not self-build
	dataSSZ, err := block.MarshalSSZ()
	require.NoError(t, err)
	return &spectypes.ProposerConsensusData{
		Duty:    *gloasProposerDuty(slot),
		Version: networkconfig.DataVersionGloas,
		DataSSZ: dataSSZ,
	}
}

// On the self-build path the proposer starts the §6 envelope-signing duty for the slot (fires on every
// operator, builder or not, so all join the envelope round).
func TestProposerRunnerSubmitGloasProposalTriggersEnvelopeOnSelfBuild(t *testing.T) {
	t.Parallel()

	const slot = phase0.Slot(8)
	consensusData := gloasProposerConsensusData(t, slot) // self-build (TestingBeaconBlock)
	runner, keySet, _ := newProposerRunnerForTest(t, newProposerTestBeacon(nil), &stubDoppelganger{canSign: true}, 0, nil)
	setupRunnerForPostConsensus(t, runner, keySet, gloasProposerDuty(slot), consensusData, 1)

	var gotSlot phase0.Slot
	called := false
	runner.startEnvelopeDuty = func(s phase0.Slot) { called, gotSlot = true, s }

	err := runner.submitGloasProposal(context.Background(), zap.NewNop(), trace.SpanFromContext(context.Background()), consensusData, phase0.BLSSignature{})
	require.NoError(t, err)
	require.True(t, called, "self-build should start the envelope duty")
	require.Equal(t, slot, gotSlot)
}

// Block built by an external builder: that builder signs its own envelope, so SSV must not start one.
func TestProposerRunnerSubmitGloasProposalSkipsEnvelopeOnExternalBuild(t *testing.T) {
	t.Parallel()

	const slot = phase0.Slot(8)
	consensusData := gloasExternalBuildConsensusData(t, slot)
	runner, keySet, _ := newProposerRunnerForTest(t, newProposerTestBeacon(nil), &stubDoppelganger{canSign: true}, 0, nil)
	setupRunnerForPostConsensus(t, runner, keySet, gloasProposerDuty(slot), consensusData, 1)

	called := false
	runner.startEnvelopeDuty = func(_ phase0.Slot) { called = true }

	err := runner.submitGloasProposal(context.Background(), zap.NewNop(), trace.SpanFromContext(context.Background()), consensusData, phase0.BLSSignature{})
	require.NoError(t, err)
	require.False(t, called, "external-build should not start the envelope duty")
}

// A BN submit error fails the duty, but the self-build envelope duty must still start — the envelope is a
// cluster round the others join regardless of this operator's block submit.
func TestProposerRunnerSubmitGloasProposalErrorStillTriggersEnvelope(t *testing.T) {
	t.Parallel()

	const slot = phase0.Slot(8)
	consensusData := gloasProposerConsensusData(t, slot) // self-build
	beacon := newProposerTestBeacon(nil)
	beacon.submitErr = errors.New("bn rejected")
	runner, keySet, _ := newProposerRunnerForTest(t, beacon, &stubDoppelganger{canSign: true}, 0, nil)
	setupRunnerForPostConsensus(t, runner, keySet, gloasProposerDuty(slot), consensusData, 1)

	called := false
	runner.startEnvelopeDuty = func(phase0.Slot) { called = true }

	err := runner.submitGloasProposal(context.Background(), zap.NewNop(), trace.SpanFromContext(context.Background()), consensusData, phase0.BLSSignature{})
	require.ErrorContains(t, err, "submit gloas beacon block")
	require.True(t, called, "envelope must still start even when the block submit fails")
}

// recordDecidedBlockRoot stores exactly block.HashTreeRoot() — the root the §6 envelope value-check
// matches against — and is a no-op without a store.
func TestProposerRunnerRecordDecidedBlockRoot(t *testing.T) {
	t.Parallel()

	runner, _, _ := newProposerRunnerForTest(t, newProposerTestBeacon(nil), &stubDoppelganger{canSign: true}, 0, nil)

	// No store (no envelope runner) → no-op, no error.
	require.NoError(t, runner.recordDecidedBlockRoot(9, gloas.TestingBeaconBlock(9)))

	store := ssv.NewProposedBlockRoots()
	runner.proposedBlockRoots = store
	block := gloas.TestingBeaconBlock(8)
	require.NoError(t, runner.recordDecidedBlockRoot(8, block))

	expectedRoot, err := block.HashTreeRoot()
	require.NoError(t, err)
	got, ok := store.Get(8)
	require.True(t, ok)
	require.Equal(t, phase0.Root(expectedRoot), got)
}
