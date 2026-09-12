package runner

import (
	"context"
	"maps"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/deneb"
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
	getGloasBuilderURL   string                  // the Eth-Builder-Url the produce returns; empty = self-build / p2p win
	getGloasEnvelope     *gloas.ProducedEnvelope // the reveal data a self-build produce returns; nil = external build
	submittedGloasBlocks []*gloas.SignedBeaconBlock
	submittedEnvelopes   []*gloas.SignedExecutionPayloadEnvelopeContents
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

func (b *proposerTestBeacon) GetGloasBeaconBlock(_ context.Context, slot phase0.Slot, graffiti, randao []byte, _ *gloas.ProduceBuilderConfig) (*gloas.ProducedBlock, error) {
	b.getCalls++
	b.lastGetSlot = slot
	b.lastGetGraffiti = append([]byte(nil), graffiti...)
	b.lastGetRandao = append([]byte(nil), randao...)
	return &gloas.ProducedBlock{Block: b.getGloasBlock, BuilderURL: b.getGloasBuilderURL, Envelope: b.getGloasEnvelope}, nil
}

func (b *proposerTestBeacon) SubmitGloasBeaconBlock(_ context.Context, block *gloas.SignedBeaconBlock, _ string) error {
	b.submittedGloasBlocks = append(b.submittedGloasBlocks, block)
	return b.submitErr
}

func (b *proposerTestBeacon) SubmitExecutionPayloadEnvelope(_ context.Context, contents *gloas.SignedExecutionPayloadEnvelopeContents) error {
	b.submittedEnvelopes = append(b.submittedEnvelopes, contents)
	return nil
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

// gloasTestConfig schedules Gloas from genesis and puts the wall clock inside slot, so a duty at that slot
// runs on the Gloas path with its awaiting-envelope window (bounded to the slot) open.
func gloasTestConfig(slot phase0.Slot) *networkconfig.Network {
	cfg := networkconfig.TestNetworkWithGloas(0)
	cfg.GenesisTime = time.Now().Add(-time.Duration(slot)*cfg.SlotDuration - time.Second)
	return cfg
}

// gloasTestEnvelope is the reveal data the builder operator's produce response carried for block: an
// envelope whose empty requests hash to the bid's requests root, with its blobs and KZG proofs.
func gloasTestEnvelope(t *testing.T, block *gloas.BeaconBlock) *gloas.ProducedEnvelope {
	t.Helper()
	blockRoot, err := block.HashTreeRoot()
	require.NoError(t, err)
	return &gloas.ProducedEnvelope{
		Envelope: &gloas.ExecutionPayloadEnvelope{
			Payload:               &gloas.ExecutionPayload{BlockNumber: 42},
			ExecutionRequests:     &gloas.ExecutionRequests{},
			BuilderIndex:          gloas.BuilderIndexSelfBuild,
			BeaconBlockRoot:       blockRoot,
			ParentBeaconBlockRoot: block.ParentRoot,
		},
		KZGProofs: []deneb.KZGProof{{0x01}},
		Blobs:     []deneb.Blob{{0x02}},
	}
}

// gloasSelfBuildProposal is the §4 value for a self-build block at slot — the block plus its envelope's
// payload_root — together with the reveal data the builder operator holds for it.
func gloasSelfBuildProposal(t *testing.T, slot phase0.Slot) (*gloas.GloasProposalData, *gloas.ProducedEnvelope) {
	t.Helper()
	block := gloas.TestingBeaconBlock(slot)
	produced := gloasTestEnvelope(t, block)
	payloadRoot, err := produced.Envelope.Payload.HashTreeRoot()
	require.NoError(t, err)
	return &gloas.GloasProposalData{Block: block, PayloadRoot: payloadRoot}, produced
}

// gloasExternalBuildProposal is the §4 value for a block won by an external builder: zero payload_root.
func gloasExternalBuildProposal(slot phase0.Slot) *gloas.GloasProposalData {
	block := gloas.TestingBeaconBlock(slot)
	block.Body.SignedExecutionPayloadBid.Message.BuilderIndex = 5 // an external builder, not self-build
	return &gloas.GloasProposalData{Block: block}
}

func gloasConsensusData(t *testing.T, proposal *gloas.GloasProposalData) *spectypes.ProposerConsensusData {
	t.Helper()
	dataSSZ, err := proposal.Encode()
	require.NoError(t, err)
	return &spectypes.ProposerConsensusData{
		Duty:    *gloasProposerDuty(proposal.Block.Slot),
		Version: networkconfig.DataVersionGloas,
		DataSSZ: dataSSZ,
	}
}

// gloasPartialSig is operator opID's partial signature over obj under domainType with its share key. The
// testing beacon's domain is epoch-invariant, so it matches the domain the runner derives at the duty's epoch.
func gloasPartialSig(t *testing.T, keySet *spectestingutils.TestKeySet, opID spectypes.OperatorID, obj spectypes.HashRoot, domainType phase0.DomainType) *spectypes.PartialSignatureMessage {
	t.Helper()
	domain, err := spectestingutils.NewTestingBeaconNode().DomainData(1, domainType)
	require.NoError(t, err)
	sig, signingRoot, err := spectestingutils.NewTestingKeyManager().SignBeaconObject(obj, domain, keySet.Shares[opID].GetPublicKey().Serialize(), domainType)
	require.NoError(t, err)
	return &spectypes.PartialSignatureMessage{
		PartialSignature: sig,
		SigningRoot:      signingRoot,
		Signer:           opID,
		ValidatorIndex:   spectestingutils.TestingValidatorIndex,
	}
}

// gloasPostConsensusMsg is operator opID's post-consensus packet for the decided Gloas value: the block root
// under DomainProposer and, when withEnvelope, the derived §6 envelope root under DomainBeaconBuilder.
func gloasPostConsensusMsg(t *testing.T, keySet *spectestingutils.TestKeySet, opID spectypes.OperatorID, proposal *gloas.GloasProposalData, withEnvelope bool) *spectypes.PartialSignatureMessages {
	t.Helper()
	entries := []*spectypes.PartialSignatureMessage{gloasPartialSig(t, keySet, opID, proposal.Block, spectypes.DomainProposer)}
	if withEnvelope {
		envelope, err := proposal.DeriveBlindedEnvelope()
		require.NoError(t, err)
		entries = append(entries, gloasPartialSig(t, keySet, opID, envelope, phase0.DomainType(spectypes.DomainBeaconBuilder)))
	}
	return &spectypes.PartialSignatureMessages{Type: spectypes.PostConsensusPartialSig, Slot: proposal.Block.Slot, Messages: entries}
}

// requireSameRoot compares two SSZ objects by hash tree root: an SSZ-decoded block carries empty lists where
// the constructed fixture has nil ones, so struct equality does not hold even for identical values.
func requireSameRoot(t *testing.T, want, got spectypes.HashRoot) {
	t.Helper()
	wantRoot, err := want.HashTreeRoot()
	require.NoError(t, err)
	gotRoot, err := got.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, wantRoot, gotRoot)
}

// newGloasProposerForPostConsensus is a proposer runner whose Gloas duty at the value's slot has decided
// on it, ready to process post-consensus packets.
func newGloasProposerForPostConsensus(t *testing.T, beacon *proposerTestBeacon, dg *stubDoppelganger, proposal *gloas.GloasProposalData) (*ProposerRunner, *spectestingutils.TestKeySet) {
	t.Helper()
	slot := proposal.Block.Slot
	runner, keySet, _ := newProposerRunnerForTest(t, beacon, dg, 0, gloasTestConfig(slot))
	setupRunnerForPostConsensus(t, runner, keySet, gloasProposerDuty(slot), gloasConsensusData(t, proposal), 1)
	return runner, keySet
}

// At a Gloas self-build slot the packet carries the block root and the §6 envelope root. When both reach
// quorum, every operator submits the block (it is bid-only, so all hold it and the BN dedupes), and the
// builder operator — whose produced envelope is the one the decided value commits to — publishes the
// reveal with its blobs and KZG proofs. The duty is finished, with no envelope left to wait for.
func TestProposerRunnerGloasPostConsensusSubmitsBlockAndPublishesEnvelope(t *testing.T) {
	t.Parallel()

	ctx, logger := context.Background(), zap.NewNop()
	proposal, produced := gloasSelfBuildProposal(t, 8)
	beacon := newProposerTestBeacon(nil)
	dg := &stubDoppelganger{canSign: true}
	runner, keySet := newGloasProposerForPostConsensus(t, beacon, dg, proposal)
	runner.gloasProducedEnvelope = produced // the builder operator

	for opID := spectypes.OperatorID(1); opID <= keySet.Threshold; opID++ {
		require.NoError(t, runner.ProcessPostConsensus(ctx, logger, gloasPostConsensusMsg(t, keySet, opID, proposal, true)))
	}

	require.Len(t, beacon.submittedGloasBlocks, 1)
	requireSameRoot(t, proposal.Block, beacon.submittedGloasBlocks[0].Message)
	require.NotEqual(t, phase0.BLSSignature{}, beacon.submittedGloasBlocks[0].Signature)
	require.Len(t, beacon.submittedEnvelopes, 1)
	published := beacon.submittedEnvelopes[0]
	require.Equal(t, produced.Envelope, published.SignedExecutionPayloadEnvelope.Message)
	require.NotEqual(t, phase0.BLSSignature{}, published.SignedExecutionPayloadEnvelope.Signature)
	require.NotEqual(t, beacon.submittedGloasBlocks[0].Signature, published.SignedExecutionPayloadEnvelope.Signature)
	require.Equal(t, produced.KZGProofs, published.KZGProofs)
	require.Equal(t, produced.Blobs, published.Blobs)
	require.Equal(t, []phase0.ValidatorIndex{runner.GetShare().ValidatorIndex}, dg.reportQuorum)
	require.True(t, runner.State.Succeeded)
	requireNotAwaitingPostConsensus(t, runner)
}

// An operator whose own produce is not the decided block reconstructs the envelope signature like everyone
// else but holds no payload behind it, so it publishes nothing.
func TestProposerRunnerGloasPostConsensusNonBuilderDoesNotPublish(t *testing.T) {
	t.Parallel()

	ctx, logger := context.Background(), zap.NewNop()
	proposal, _ := gloasSelfBuildProposal(t, 8)
	beacon := newProposerTestBeacon(nil)
	runner, keySet := newGloasProposerForPostConsensus(t, beacon, &stubDoppelganger{canSign: true}, proposal)
	// This operator produced another block: its envelope blinds to a different root.
	other, _ := gloasSelfBuildProposal(t, 8)
	other.Block.ProposerIndex = 99
	runner.gloasProducedEnvelope = gloasTestEnvelope(t, other.Block)

	for opID := spectypes.OperatorID(1); opID <= keySet.Threshold; opID++ {
		require.NoError(t, runner.ProcessPostConsensus(ctx, logger, gloasPostConsensusMsg(t, keySet, opID, proposal, true)))
	}

	require.Len(t, beacon.submittedGloasBlocks, 1)
	require.Empty(t, beacon.submittedEnvelopes)
	require.True(t, runner.State.Succeeded)
	requireNotAwaitingPostConsensus(t, runner)
}

// The envelope root is optional per packet and reconstructs independently: when the block reaches quorum
// first (a peer signed the block alone), the duty finishes but keeps accepting post-consensus packets until
// the envelope reconstructs, so the reveal is not lost (SIP #94 §4).
func TestProposerRunnerGloasPostConsensusLateEnvelope(t *testing.T) {
	t.Parallel()

	ctx, logger := context.Background(), zap.NewNop()
	proposal, produced := gloasSelfBuildProposal(t, 8)
	beacon := newProposerTestBeacon(nil)
	runner, keySet := newGloasProposerForPostConsensus(t, beacon, &stubDoppelganger{canSign: true}, proposal)
	runner.gloasProducedEnvelope = produced

	require.NoError(t, runner.ProcessPostConsensus(ctx, logger, gloasPostConsensusMsg(t, keySet, 1, proposal, true)))
	require.NoError(t, runner.ProcessPostConsensus(ctx, logger, gloasPostConsensusMsg(t, keySet, 2, proposal, true)))
	require.NoError(t, runner.ProcessPostConsensus(ctx, logger, gloasPostConsensusMsg(t, keySet, 3, proposal, false))) // block alone

	// Block quorum: submitted and finished, envelope still one short — the duty keeps that slot's
	// post-consensus packets flowing from the queue.
	require.Len(t, beacon.submittedGloasBlocks, 1)
	require.Empty(t, beacon.submittedEnvelopes)
	require.True(t, runner.State.Succeeded)
	require.False(t, runner.HasRunningDuty())
	awaitingSlot, awaiting := runner.AwaitingPostConsensus()
	require.True(t, awaiting)
	require.Equal(t, phase0.Slot(8), awaitingSlot)

	require.NoError(t, runner.ProcessPostConsensus(ctx, logger, gloasPostConsensusMsg(t, keySet, 4, proposal, true)))

	require.Len(t, beacon.submittedGloasBlocks, 1, "the block quorum fires once")
	require.Len(t, beacon.submittedEnvelopes, 1)
	require.Equal(t, produced.Envelope, beacon.submittedEnvelopes[0].SignedExecutionPayloadEnvelope.Message)
	requireNotAwaitingPostConsensus(t, runner)
}

// requireNotAwaitingPostConsensus checks the runner expects no further post-consensus packets.
func requireNotAwaitingPostConsensus(t *testing.T, runner *ProposerRunner) {
	t.Helper()
	require.False(t, runner.awaitingEnvelope())
	_, awaiting := runner.AwaitingPostConsensus()
	require.False(t, awaiting)
}

// The awaiting-envelope window closes with the duty's slot: a reveal after it is useless, so the finished
// duty goes back to rejecting post-consensus packets.
func TestProposerRunnerGloasAwaitingEnvelopeClosesWithSlot(t *testing.T) {
	t.Parallel()

	ctx, logger := context.Background(), zap.NewNop()
	proposal, produced := gloasSelfBuildProposal(t, 8)
	runner, keySet := newGloasProposerForPostConsensus(t, newProposerTestBeacon(nil), &stubDoppelganger{canSign: true}, proposal)
	runner.gloasProducedEnvelope = produced

	for opID := spectypes.OperatorID(1); opID <= keySet.Threshold; opID++ {
		require.NoError(t, runner.ProcessPostConsensus(ctx, logger, gloasPostConsensusMsg(t, keySet, opID, proposal, false)))
	}
	require.True(t, runner.State.Succeeded)
	require.True(t, runner.awaitingEnvelope())

	// The clock moves into the next slot.
	runner.NetworkConfig.GenesisTime = runner.NetworkConfig.GenesisTime.Add(-runner.NetworkConfig.SlotDuration)
	requireNotAwaitingPostConsensus(t, runner)

	err := runner.ProcessPostConsensus(ctx, logger, gloasPostConsensusMsg(t, keySet, 4, proposal, true))
	require.ErrorContains(t, err, ErrRunningDutySucceeded.Error())
}

// An external bid expects the block root alone: no envelope is awaited once the block is submitted, and a
// packet carrying a second entry is rejected.
func TestProposerRunnerGloasPostConsensusExternalBuild(t *testing.T) {
	t.Parallel()

	ctx, logger := context.Background(), zap.NewNop()
	proposal := gloasExternalBuildProposal(8)
	beacon := newProposerTestBeacon(nil)
	runner, keySet := newGloasProposerForPostConsensus(t, beacon, &stubDoppelganger{canSign: true}, proposal)

	twoEntries := gloasPostConsensusMsg(t, keySet, 1, proposal, false)
	twoEntries.Messages = append(twoEntries.Messages, gloasPartialSig(t, keySet, 1, spectypes.SSZ32Bytes{0xee}, phase0.DomainType(spectypes.DomainBeaconBuilder)))
	require.ErrorContains(t, runner.ProcessPostConsensus(ctx, logger, twoEntries), "wrong expected roots count")

	for opID := spectypes.OperatorID(1); opID <= keySet.Threshold; opID++ {
		require.NoError(t, runner.ProcessPostConsensus(ctx, logger, gloasPostConsensusMsg(t, keySet, opID, proposal, false)))
	}

	require.Len(t, beacon.submittedGloasBlocks, 1)
	require.Empty(t, beacon.submittedEnvelopes)
	require.True(t, runner.State.Succeeded)
	requireNotAwaitingPostConsensus(t, runner)
}

// A self-build packet must carry the block root, may carry the envelope root, and nothing else (SIP #94 §4/§7).
func TestProposerRunnerGloasPostConsensusRejectsMalformedPackets(t *testing.T) {
	t.Parallel()

	ctx, logger := context.Background(), zap.NewNop()
	proposal, _ := gloasSelfBuildProposal(t, 8)
	runner, keySet := newGloasProposerForPostConsensus(t, newProposerTestBeacon(nil), &stubDoppelganger{canSign: true}, proposal)
	envelope, err := proposal.DeriveBlindedEnvelope()
	require.NoError(t, err)
	builderDomain := phase0.DomainType(spectypes.DomainBeaconBuilder)

	envelopeOnly := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     8,
		Messages: []*spectypes.PartialSignatureMessage{gloasPartialSig(t, keySet, 1, envelope, builderDomain)},
	}
	require.ErrorContains(t, runner.ProcessPostConsensus(ctx, logger, envelopeOnly), "missing required signing root")

	threeEntries := gloasPostConsensusMsg(t, keySet, 1, proposal, true)
	threeEntries.Messages = append(threeEntries.Messages, gloasPartialSig(t, keySet, 1, spectypes.SSZ32Bytes{0xee}, builderDomain))
	require.ErrorContains(t, runner.ProcessPostConsensus(ctx, logger, threeEntries), "wrong expected roots count")

	unexpectedRoot := gloasPostConsensusMsg(t, keySet, 1, proposal, false)
	unexpectedRoot.Messages = append(unexpectedRoot.Messages, gloasPartialSig(t, keySet, 1, spectypes.SSZ32Bytes{0xee}, builderDomain))
	require.ErrorContains(t, runner.ProcessPostConsensus(ctx, logger, unexpectedRoot), "unexpected signing root")
}

// On a Gloas self-build decision the proposer signs the block root under DomainProposer and the derived §6
// envelope root under DomainBeaconBuilder, in one post-consensus packet (SIP #94 §4/§6).
func TestProposerRunnerProcessConsensusGloasSignsBlockAndEnvelope(t *testing.T) {
	t.Parallel()

	const slot = phase0.Slot(8)
	ctx, logger := context.Background(), zap.NewNop()
	runner, keySet, network := newProposerRunnerForTest(t, newProposerTestBeacon(nil), &stubDoppelganger{canSign: true}, 0, gloasTestConfig(slot))
	require.NoError(t, runner.StartNewDuty(ctx, logger, gloasProposerDuty(slot), keySet.Threshold))

	proposal, _ := gloasSelfBuildProposal(t, slot)
	consensusData := gloasConsensusData(t, proposal)
	runner.measurements.StartConsensus()
	require.NoError(t, runner.decide(ctx, logger, slot, consensusData, runner.ValCheck))
	for _, msg := range spectestingutils.SSVDecidingMsgsForHeight(consensusData, runner.QBFTController.GetIdentifier(), specqbft.Height(slot), keySet) {
		require.NoError(t, runner.ProcessConsensus(ctx, logger, msg))
	}

	var packet *spectypes.PartialSignatureMessages
	for _, msg := range network.BroadcastedMsgs {
		if msg.SSVMessage == nil || msg.SSVMessage.MsgType != spectypes.SSVPartialSignatureMsgType {
			continue
		}
		decoded := &spectypes.PartialSignatureMessages{}
		require.NoError(t, decoded.Decode(msg.SSVMessage.Data))
		if decoded.Type == spectypes.PostConsensusPartialSig {
			packet = decoded
		}
	}
	require.NotNil(t, packet)
	require.Len(t, packet.Messages, 2)
	blockRoot, envelopeRoot, err := runner.gloasPostConsensusSigningRoots(ctx)
	require.NoError(t, err)
	require.Equal(t, blockRoot, packet.Messages[0].SigningRoot)
	require.Equal(t, envelopeRoot, packet.Messages[1].SigningRoot)
	require.NotEqual(t, blockRoot, envelopeRoot)
}

// The expected post-consensus roots: the block under DomainProposer always; on a self-build value also the
// derived §6 envelope under DomainBeaconBuilder, optional; a single required root pre-Gloas.
func TestProposerRunnerExpectedPostConsensusRootsAndDomains(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	proposal, _ := gloasSelfBuildProposal(t, 8)
	runner, _ := newGloasProposerForPostConsensus(t, newProposerTestBeacon(nil), &stubDoppelganger{canSign: true}, proposal)

	roots, err := runner.expectedPostConsensusRootsAndDomains(ctx)
	require.NoError(t, err)
	require.Len(t, roots, 2)
	requireSameRoot(t, proposal.Block, roots[0].Root)
	require.Equal(t, phase0.DomainType(spectypes.DomainProposer), roots[0].Domain)
	require.False(t, roots[0].Optional)
	envelope, err := proposal.DeriveBlindedEnvelope()
	require.NoError(t, err)
	requireSameRoot(t, envelope, roots[1].Root)
	require.Equal(t, phase0.DomainType(spectypes.DomainBeaconBuilder), roots[1].Domain)
	require.True(t, roots[1].Optional)

	external := gloasExternalBuildProposal(8)
	runner, _ = newGloasProposerForPostConsensus(t, newProposerTestBeacon(nil), &stubDoppelganger{canSign: true}, external)
	roots, err = runner.expectedPostConsensusRootsAndDomains(ctx)
	require.NoError(t, err)
	require.Len(t, roots, 1)
	requireSameRoot(t, external.Block, roots[0].Root)
	require.Equal(t, phase0.DomainType(spectypes.DomainProposer), roots[0].Domain)
	require.False(t, roots[0].Optional)

	version := spec.DataVersionDeneb
	preGloas, keySet, _ := newProposerRunnerForTest(t, newProposerTestBeacon(spectestingutils.TestingBeaconBlockV(version)), &stubDoppelganger{canSign: true}, 0, nil)
	setupRunnerForPostConsensus(t, preGloas, keySet, spectestingutils.TestingProposerDutyV(version), spectestingutils.TestProposerBlindedBlockConsensusDataV(version), 1)
	roots, err = preGloas.expectedPostConsensusRootsAndDomains(ctx)
	require.NoError(t, err)
	require.Len(t, roots, 1)
	require.Equal(t, phase0.DomainType(spectypes.DomainProposer), roots[0].Domain)
	require.False(t, roots[0].Optional)
}

// gloasProposalInput wraps the produced block as the §4 value: on a self-build produce the payload_root is
// the envelope payload's root and the reveal data is kept for publishEnvelope; on an external bid the
// payload_root is zero and only the builder URL is kept (for decidedBuilderURL).
func TestProposerRunnerGloasProposalInput(t *testing.T) {
	t.Parallel()

	const slot = phase0.Slot(8)
	ctx, logger := context.Background(), zap.NewNop()

	t.Run("self-build", func(t *testing.T) {
		beacon := newProposerTestBeacon(nil)
		beacon.getGloasBlock = gloas.TestingBeaconBlock(slot)
		beacon.getGloasEnvelope = gloasTestEnvelope(t, beacon.getGloasBlock)
		runner, _, _ := newProposerRunnerForTest(t, beacon, &stubDoppelganger{canSign: true}, 0, nil)

		input, err := runner.gloasProposalInput(ctx, logger, gloasProposerDuty(slot), []byte("randao"))
		require.NoError(t, err)
		require.Equal(t, networkconfig.DataVersionGloas, input.Version)
		require.Equal(t, slot, beacon.lastGetSlot)
		require.Equal(t, []byte("graffiti"), beacon.lastGetGraffiti)
		require.Equal(t, []byte("randao"), beacon.lastGetRandao)

		decoded, err := gloas.DecodeGloasProposalData(input.DataSSZ)
		require.NoError(t, err)
		requireSameRoot(t, beacon.getGloasBlock, decoded.Block)
		payloadRoot, err := beacon.getGloasEnvelope.Envelope.Payload.HashTreeRoot()
		require.NoError(t, err)
		require.Equal(t, phase0.Root(payloadRoot), decoded.PayloadRoot)

		expectedRoot, err := beacon.getGloasBlock.HashTreeRoot()
		require.NoError(t, err)
		require.Equal(t, expectedRoot, runner.gloasProducedRoot)
		require.Empty(t, runner.gloasBuilderURL)
		require.Same(t, beacon.getGloasEnvelope, runner.gloasProducedEnvelope)
	})

	t.Run("external bid", func(t *testing.T) {
		beacon := newProposerTestBeacon(nil)
		beacon.getGloasBlock = gloasExternalBuildProposal(slot).Block
		beacon.getGloasBuilderURL = "https://b.example"
		runner, _, _ := newProposerRunnerForTest(t, beacon, &stubDoppelganger{canSign: true}, 0, nil)

		input, err := runner.gloasProposalInput(ctx, logger, gloasProposerDuty(slot), []byte("randao"))
		require.NoError(t, err)
		decoded, err := gloas.DecodeGloasProposalData(input.DataSSZ)
		require.NoError(t, err)
		require.Equal(t, phase0.Root{}, decoded.PayloadRoot)
		require.Equal(t, "https://b.example", runner.gloasBuilderURL)
		require.Nil(t, runner.gloasProducedEnvelope)
	})

	// A self-build produce without its payload has no payload_root to decide on; the value check would
	// reject such a value, so the produce fails instead.
	t.Run("self-build without payload", func(t *testing.T) {
		beacon := newProposerTestBeacon(nil)
		beacon.getGloasBlock = gloas.TestingBeaconBlock(slot)
		runner, _, _ := newProposerRunnerForTest(t, beacon, &stubDoppelganger{canSign: true}, 0, nil)

		_, err := runner.gloasProposalInput(ctx, logger, gloasProposerDuty(slot), []byte("randao"))
		require.ErrorContains(t, err, "without its payload")
	})
}

// StartNewDuty clears the previous duty's Gloas produce markers along with the cached pre-Gloas block, so a
// stale owner-match can't echo an old Eth-Builder-Url on the next proposal and a stale envelope root can't
// hold the new duty open.
func TestProposerRunnerStartNewDutyResetsGloasProduceMarkers(t *testing.T) {
	t.Parallel()

	version := spec.DataVersionDeneb
	beacon := newProposerTestBeacon(spectestingutils.TestingBeaconBlockV(version))
	runner, _, _ := newProposerRunnerForTest(t, beacon, &stubDoppelganger{canSign: true}, 0, nil)
	runner.gloasProducedRoot = [32]byte{0xaa}
	runner.gloasBuilderURL = "https://stale.example"
	runner.gloasProducedEnvelope = &gloas.ProducedEnvelope{}
	runner.gloasEnvelopeSigningRoot = [32]byte{0xbb}

	require.NoError(t, runner.StartNewDuty(context.Background(), zap.NewNop(), spectestingutils.TestingProposerDutyV(version), 3))

	require.Equal(t, [32]byte{}, runner.gloasProducedRoot)
	require.Empty(t, runner.gloasBuilderURL)
	require.Nil(t, runner.gloasProducedEnvelope)
	require.Equal(t, [32]byte{}, runner.gloasEnvelopeSigningRoot)
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
