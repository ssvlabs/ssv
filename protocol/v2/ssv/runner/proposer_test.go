package runner

import (
	"context"
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
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	blindutil "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon/blind"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
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

	setupRunnerForPostConsensus(t, runner, keySet, consensusData, 1)
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

	setupRunnerForPostConsensus(t, runner, keySet, consensusData, 1)
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

	setupRunnerForPostConsensus(t, runner, keySet, consensusData, 1)
	runner.operatorSigner = fixedOperatorSigner{id: 2}
	runner.cachedFullBlock = spectestingutils.TestingBeaconBlockV(version)
	runner.cachedBlindedBlockSSZ = append([]byte(nil), consensusData.DataSSZ...)

	processPostConsensusQuorum(t, runner, keySet, version)

	require.Len(t, beacon.submittedBlocks, 1)
	require.True(t, beacon.submittedBlocks[0].Blinded)
	require.Equal(t, []phase0.ValidatorIndex{runner.GetShare().ValidatorIndex}, dg.reportQuorum)
	require.True(t, runner.State.Succeeded)
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
	identifier := spectypes.NewMsgID(spectypes.JatoTestnet, spectestingutils.TestingValidatorPubKey[:], spectypes.RoleProposer)
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
	consensusData *spectypes.ProposerConsensusData,
	leaderID spectypes.OperatorID,
) {
	t.Helper()

	duty := spectestingutils.TestingProposerDutyV(consensusData.Version)
	runner.State = NewRunnerState(keySet.Threshold, duty)
	runner.measurements.StartDutyFlow()
	runner.measurements.StartConsensus()
	runner.measurements.EndConsensus()
	runner.measurements.StartPostConsensus()

	encodedDecidedValue, err := consensusData.Encode()
	require.NoError(t, err)
	runner.State.DecidedValue = encodedDecidedValue

	msgID := spectypes.NewMsgID(runner.NetworkConfig.DomainType, runner.GetShare().ValidatorPubKey[:], runner.RunnerRoleType)
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
