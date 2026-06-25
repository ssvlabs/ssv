package runner

import (
	"context"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

type guardCall struct {
	role      spectypes.BeaconRole
	validator spectypes.ValidatorPK
	slot      phase0.Slot
}

type committeeDutyGuardStub struct {
	mu         sync.Mutex
	startCalls []guardCall
	validErrs  map[string]error
}

func (s *committeeDutyGuardStub) StartDuty(role spectypes.BeaconRole, validator spectypes.ValidatorPK, slot phase0.Slot) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.startCalls = append(s.startCalls, guardCall{role: role, validator: validator, slot: slot})
	return nil
}

func (s *committeeDutyGuardStub) ValidDuty(role spectypes.BeaconRole, validator spectypes.ValidatorPK, slot phase0.Slot) error {
	if s == nil || s.validErrs == nil {
		return nil
	}
	return s.validErrs[s.validKey(role, validator, slot)]
}

func (s *committeeDutyGuardStub) validKey(role spectypes.BeaconRole, validator spectypes.ValidatorPK, slot phase0.Slot) string {
	return fmt.Sprintf("%s:%s:%d", role.String(), hex.EncodeToString(validator[:]), slot)
}

type doppelgangerStub struct {
	mu      sync.Mutex
	blocked map[phase0.ValidatorIndex]bool
	reports []phase0.ValidatorIndex
}

func (d *doppelgangerStub) CanSign(validatorIndex phase0.ValidatorIndex) bool {
	if d == nil || d.blocked == nil {
		return true
	}
	return !d.blocked[validatorIndex]
}

func (d *doppelgangerStub) ReportQuorum(validatorIndex phase0.ValidatorIndex) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.reports = append(d.reports, validatorIndex)
}

type committeeRunnerEnv struct {
	logger     *zap.Logger
	runner     *CommitteeRunner
	beacon     *protocoltesting.BeaconNodeWrapped
	network    *protocoltesting.TestingNetwork
	keySetMap  map[phase0.ValidatorIndex]*spectestingutils.TestKeySet
	sampleKey  *spectestingutils.TestKeySet
	controller *controller.Controller
}

func newCommitteeRunnerEnv(
	t *testing.T,
	validatorIndices []int,
	guard CommitteeDutyGuard,
	doppelganger DoppelgangerProvider,
) *committeeRunnerEnv {
	t.Helper()
	return newCommitteeRunnerEnvInternal(t, validatorIndices, guard, doppelganger, nil)
}

// newCommitteeRunnerEnvWithBeacon mirrors newCommitteeRunnerEnv but wires the runner to the supplied
// beacon node, so a test can substitute a faulty one to exercise submit-failure classification.
func newCommitteeRunnerEnvWithBeacon(
	t *testing.T,
	validatorIndices []int,
	beaconNode beacon.BeaconNode,
) *committeeRunnerEnv {
	t.Helper()
	return newCommitteeRunnerEnvInternal(t, validatorIndices, &committeeDutyGuardStub{}, &doppelgangerStub{}, beaconNode)
}

func newCommitteeRunnerEnvInternal(
	t *testing.T,
	validatorIndices []int,
	guard CommitteeDutyGuard,
	doppelganger DoppelgangerProvider,
	beaconNode beacon.BeaconNode,
) *committeeRunnerEnv {
	t.Helper()

	keySetMap := spectestingutils.KeySetMapForValidatorIndexList(validatorIndices)
	sorted := make([]int, 0, len(validatorIndices))
	sorted = append(sorted, validatorIndices...)
	sort.Ints(sorted)

	sampleKey := keySetMap[phase0.ValidatorIndex(sorted[0])]
	shareMap := spectestingutils.ShareMapFromKeySetMap(keySetMap)
	sharePubKeys := make([]phase0.BLSPubKey, 0, len(shareMap))
	for _, validatorIndex := range sorted {
		sharePubKeys = append(sharePubKeys, phase0.BLSPubKey(shareMap[phase0.ValidatorIndex(validatorIndex)].SharePubKey))
	}

	msgID := spectestingutils.CommitteeMsgID(sampleKey)
	network := protocoltesting.NewTestingNetwork(1, sampleKey.OperatorKeys[1])
	logger := zaptest.NewLogger(t)
	signer := ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager())

	config := protocoltesting.TestingConfig(logger, sampleKey)
	config.Network = network
	controller := protocoltesting.NewTestingQBFTController(
		sampleKey,
		msgID,
		spectestingutils.TestingCommitteeMember(sampleKey),
		config,
		false,
	)

	defaultBeacon := protocoltesting.NewTestingBeaconNodeWrapped().(*protocoltesting.BeaconNodeWrapped)
	if guard == nil {
		guard = &committeeDutyGuardStub{}
	}
	if doppelganger == nil {
		doppelganger = &doppelgangerStub{}
	}

	// The runner talks to the injected beacon when supplied (e.g. a faulty one), otherwise the plain
	// testing wrapper. env.beacon keeps the base wrapper so broadcast assertions still work.
	runnerBeacon := beaconNode
	if runnerBeacon == nil {
		runnerBeacon = defaultBeacon
	}

	runnerI, err := NewCommitteeRunner(CommitteeRunnerOptions{
		BaseRunnerOptions: BaseRunnerOptions{
			NetworkConfig:  networkconfig.TestNetwork,
			Share:          shareMap,
			Beacon:         runnerBeacon,
			Network:        network,
			Signer:         signer,
			OperatorSigner: spectestingutils.NewOperatorSigner(sampleKey, 1),
		},
		AttestingValidators: sharePubKeys,
		QBFTController:      controller,
		DutyGuard:           guard,
		DoppelgangerHandler: doppelganger,
	})
	require.NoError(t, err)

	crunner := runnerI.(*CommitteeRunner)
	crunner.SetQBFTRoundTimerF(func(_ context.Context, _ *zap.Logger, _ phase0.Slot) ssv.QBFTRoundTimer {
		return roundtimer.NewTestingTimer()
	})

	return &committeeRunnerEnv{
		logger:     logger,
		runner:     crunner,
		beacon:     defaultBeacon,
		network:    network,
		keySetMap:  keySetMap,
		sampleKey:  sampleKey,
		controller: controller,
	}
}

func (e *committeeRunnerEnv) startAndDecideCommitteeDuty(t *testing.T, duty *spectypes.CommitteeDuty) {
	t.Helper()

	// t.Context: StartNewDuty spawns a deadline watcher that may log at slot end; the test-scoped
	// context releases it before the zaptest logger becomes invalid.
	ctx := t.Context()
	require.NoError(t, e.runner.StartNewDuty(ctx, e.logger, duty, e.sampleKey.Threshold))

	for _, msg := range spectestingutils.CommitteeInputForDuty(duty, duty.Slot, e.keySetMap, false) {
		require.NoError(t, e.runner.ProcessConsensus(ctx, e.logger, msg))
	}
}

func decodeBroadcastedPartialSig(t *testing.T, msg *spectypes.SignedSSVMessage) *spectypes.PartialSignatureMessages {
	t.Helper()

	psig := &spectypes.PartialSignatureMessages{}
	require.NotNil(t, msg)
	require.NotNil(t, msg.SSVMessage)
	require.NoError(t, psig.Decode(msg.SSVMessage.Data))
	return psig
}

func partialSigBroadcasts(messages []*spectypes.SignedSSVMessage) []*spectypes.SignedSSVMessage {
	filtered := make([]*spectypes.SignedSSVMessage, 0)
	for _, msg := range messages {
		if msg != nil && msg.SSVMessage != nil && msg.SSVMessage.MsgType == spectypes.SSVPartialSignatureMsgType {
			filtered = append(filtered, msg)
		}
	}
	return filtered
}

func rootsAsHex(roots [][32]byte) []string {
	ret := make([]string, 0, len(roots))
	for _, root := range roots {
		ret = append(ret, hex.EncodeToString(root[:]))
	}
	sort.Strings(ret)
	return ret
}

func beaconRootsAsHex(roots []phase0.Root) []string {
	ret := make([]string, 0, len(roots))
	for _, root := range roots {
		ret = append(ret, hex.EncodeToString(root[:]))
	}
	sort.Strings(ret)
	return ret
}

func TestConstructAttestationData(t *testing.T) {
	vote := &spectypes.BeaconVote{
		BlockRoot: spectestingutils.TestBeaconVote.BlockRoot,
		Source:    spectestingutils.TestBeaconVote.Source,
		Target:    spectestingutils.TestBeaconVote.Target,
	}
	duty := &spectypes.ValidatorDuty{
		Slot:           spectestingutils.TestingDutySlotV(spec.DataVersionDeneb),
		CommitteeIndex: spectestingutils.TestingCommitteeIndex,
	}

	t.Run("pre electra keeps committee index", func(t *testing.T) {
		attData := constructAttestationData(vote, duty, spec.DataVersionDeneb, nil)
		require.Equal(t, spectestingutils.TestingCommitteeIndex, attData.Index)
		require.Equal(t, duty.Slot, attData.Slot)
		require.Equal(t, vote.BlockRoot, attData.BeaconBlockRoot)
	})

	t.Run("electra zeros committee index", func(t *testing.T) {
		attData := constructAttestationData(vote, duty, spec.DataVersionElectra, nil)
		require.Zero(t, attData.Index)
		require.Equal(t, duty.Slot, attData.Slot)
		require.Equal(t, vote.BlockRoot, attData.BeaconBlockRoot)
	})

	t.Run("gloas uses the decided payload-status index", func(t *testing.T) {
		index := phase0.CommitteeIndex(1)
		// The Gloas index overrides the Electra zero (SIP #94 §2) — it is the value that gets signed.
		attData := constructAttestationData(vote, duty, spec.DataVersionFulu, &index)
		require.Equal(t, index, attData.Index)
		require.Equal(t, duty.Slot, attData.Slot)
		require.Equal(t, vote.BlockRoot, attData.BeaconBlockRoot)
	})
}

func TestCommitteeRunnerStartNewDuty_StartsGuardAndResetsSubmissions(t *testing.T) {
	guard := &committeeDutyGuardStub{}
	env := newCommitteeRunnerEnv(t, []int{1, 2}, guard, &doppelgangerStub{})

	duty := spectestingutils.TestingCommitteeDuty([]int{1}, []int{2}, spec.DataVersionPhase0)

	env.runner.RecordSubmission(spectypes.BNRoleAttester, 1)
	env.runner.RecordSubmission(spectypes.BNRoleSyncCommittee, 2)

	// t.Context releases the duty deadline watcher before the zaptest logger becomes invalid.
	require.NoError(t, env.runner.StartNewDuty(t.Context(), env.logger, duty, env.sampleKey.Threshold))

	require.Len(t, guard.startCalls, 2)
	gotRoles := make([]spectypes.BeaconRole, 0, len(guard.startCalls))
	for _, call := range guard.startCalls {
		gotRoles = append(gotRoles, call.role)
		require.Equal(t, duty.Slot, call.slot)
	}
	require.ElementsMatch(t, []spectypes.BeaconRole{
		spectypes.BNRoleAttester,
		spectypes.BNRoleSyncCommittee,
	}, gotRoles)
	require.False(t, env.runner.HasSubmitted(spectypes.BNRoleAttester, 1))
	require.False(t, env.runner.HasSubmitted(spectypes.BNRoleSyncCommittee, 2))
	require.NotNil(t, env.runner.State.RunningInstance)
}

func TestCommitteeRunnerExecuteDuty_FetchesAttestationDataAndStartsConsensus(t *testing.T) {
	env := newCommitteeRunnerEnv(t, []int{1}, &committeeDutyGuardStub{}, &doppelgangerStub{})
	duty := spectestingutils.TestingAttesterDuty(spec.DataVersionElectra)

	env.runner.State = NewRunnerState(env.sampleKey.Threshold, duty)

	require.NoError(t, env.runner.executeDuty(context.Background(), env.logger, duty))
	require.NotNil(t, env.runner.ValCheck)
	require.NotNil(t, env.runner.State.RunningInstance)
	require.Equal(t, specqbft.Height(duty.Slot), env.runner.State.RunningInstance.GetHeight())

	expectedVote := &spectypes.BeaconVote{
		BlockRoot: spectestingutils.TestBeaconVote.BlockRoot,
		Source:    spectestingutils.TestBeaconVote.Source,
		Target:    spectestingutils.TestBeaconVote.Target,
	}
	expectedVoteBytes, err := expectedVote.Encode()
	require.NoError(t, err)
	require.NoError(t, env.runner.ValCheck.CheckValue(expectedVoteBytes))
}

func TestCommitteeRunnerProcessConsensus_UsesWorkerPoolForMoreThan30SyncDuties(t *testing.T) {
	validatorIndices := make([]int, 35)
	for i := range validatorIndices {
		validatorIndices[i] = i + 1
	}
	env := newCommitteeRunnerEnv(t, validatorIndices, &committeeDutyGuardStub{}, &doppelgangerStub{})
	duty := spectestingutils.TestingCommitteeDuty(nil, validatorIndices, spec.DataVersionPhase0)

	env.startAndDecideCommitteeDuty(t, duty)

	partialSigMsgs := partialSigBroadcasts(env.network.BroadcastedMsgs)
	require.Len(t, partialSigMsgs, 1)
	psig := decodeBroadcastedPartialSig(t, partialSigMsgs[0])
	require.Len(t, psig.Messages, len(validatorIndices))

	gotValidatorIndices := make([]phase0.ValidatorIndex, 0, len(psig.Messages))
	for _, msg := range psig.Messages {
		require.Equal(t, spectypes.OperatorID(1), msg.Signer)
		gotValidatorIndices = append(gotValidatorIndices, msg.ValidatorIndex)
	}

	expectedValidatorIndices := make([]phase0.ValidatorIndex, 0, len(validatorIndices))
	for _, idx := range validatorIndices {
		expectedValidatorIndices = append(expectedValidatorIndices, phase0.ValidatorIndex(idx))
	}
	require.ElementsMatch(t, expectedValidatorIndices, gotValidatorIndices)
}

func TestCommitteeRunnerProcessConsensus_DoppelgangerAndDutyBranching(t *testing.T) {
	t.Run("all attesters blocked with no sync duties skips broadcast", func(t *testing.T) {
		doppelganger := &doppelgangerStub{
			blocked: map[phase0.ValidatorIndex]bool{
				1: true,
				2: true,
				3: true,
			},
		}
		env := newCommitteeRunnerEnv(t, []int{1, 2, 3}, &committeeDutyGuardStub{}, doppelganger)
		duty := spectestingutils.TestingCommitteeDuty([]int{1, 2, 3}, nil, spec.DataVersionPhase0)

		env.startAndDecideCommitteeDuty(t, duty)

		require.Empty(t, partialSigBroadcasts(env.network.BroadcastedMsgs))
	})

	t.Run("blocked attesters do not block sync committee signing", func(t *testing.T) {
		doppelganger := &doppelgangerStub{
			blocked: map[phase0.ValidatorIndex]bool{
				1: true,
			},
		}
		env := newCommitteeRunnerEnv(t, []int{1, 2}, &committeeDutyGuardStub{}, doppelganger)
		duty := spectestingutils.TestingCommitteeDuty([]int{1}, []int{2}, spec.DataVersionPhase0)

		env.startAndDecideCommitteeDuty(t, duty)

		partialSigMsgs := partialSigBroadcasts(env.network.BroadcastedMsgs)
		require.Len(t, partialSigMsgs, 1)
		psig := decodeBroadcastedPartialSig(t, partialSigMsgs[0])
		require.Len(t, psig.Messages, 1)
		require.Equal(t, phase0.ValidatorIndex(2), psig.Messages[0].ValidatorIndex)
	})
}

func TestCommitteeRunnerProcessPostConsensus_SubmitsElectraObjectsAndDeduplicates(t *testing.T) {
	doppelganger := &doppelgangerStub{}
	env := newCommitteeRunnerEnv(t, []int{1, 2}, &committeeDutyGuardStub{}, doppelganger)
	duty := spectestingutils.TestingCommitteeDuty([]int{1}, []int{2}, spec.DataVersionElectra)

	env.startAndDecideCommitteeDuty(t, duty)

	postConsensusMsgs := []*spectypes.PartialSignatureMessages{
		spectestingutils.PostConsensusCommitteeMsgForDuty(duty, env.keySetMap, 1),
		spectestingutils.PostConsensusCommitteeMsgForDuty(duty, env.keySetMap, 2),
		spectestingutils.PostConsensusCommitteeMsgForDuty(duty, env.keySetMap, 3),
	}

	for i, msg := range postConsensusMsgs {
		require.NoError(t, env.runner.ProcessPostConsensus(context.Background(), env.logger, msg))
		if i < 2 {
			require.Empty(t, env.beacon.GetBroadcastedRoots())
		}
	}

	require.True(t, env.runner.State.Succeeded)
	require.Len(t, env.beacon.GetBroadcastedRoots(), 2)

	attesterDuty := duty.ValidatorDuties[0]
	syncDuty := duty.ValidatorDuties[1]

	expectedAttRoot, err := spectestingutils.TestingElectraSingleAttestationForDuty(env.keySetMap[attesterDuty.ValidatorIndex], attesterDuty).HashTreeRoot()
	require.NoError(t, err)
	expectedSyncRoot, err := spectestingutils.TestingSignedSyncCommitteeBlockRootForValidatorIndex(
		env.keySetMap[syncDuty.ValidatorIndex],
		syncDuty.ValidatorIndex,
		spec.DataVersionElectra,
	).HashTreeRoot()
	require.NoError(t, err)

	require.Equal(
		t,
		rootsAsHex([][32]byte{expectedAttRoot, expectedSyncRoot}),
		beaconRootsAsHex(env.beacon.GetBroadcastedRoots()),
	)
	require.ElementsMatch(t, []phase0.ValidatorIndex{attesterDuty.ValidatorIndex}, doppelganger.reports)

	err = env.runner.ProcessPostConsensus(context.Background(), env.logger, postConsensusMsgs[2])
	require.ErrorContains(t, err, ErrRunningDutySucceeded.Error())
	require.Len(t, env.beacon.GetBroadcastedRoots(), 2)
}
