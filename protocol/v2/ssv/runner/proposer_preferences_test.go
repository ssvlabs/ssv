package runner

import (
	"context"
	"fmt"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

type errFeeRecipientProvider struct{}

func (errFeeRecipientProvider) GetFeeRecipient(spectypes.ValidatorPK) (bellatrix.ExecutionAddress, error) {
	return bellatrix.ExecutionAddress{}, fmt.Errorf("no fee recipient")
}

// fixedFeeRecipientProvider returns the same fee recipient for every validator, so every "operator"
// in a test freezes byte-identical preferences.
type fixedFeeRecipientProvider struct{ addr bellatrix.ExecutionAddress }

func (p fixedFeeRecipientProvider) GetFeeRecipient(spectypes.ValidatorPK) (bellatrix.ExecutionAddress, error) {
	return p.addr, nil
}

// prefsTestBeacon embeds the spec testing beacon (so DomainData resolves) while stubbing the §5
// surface: a settable dependent root and a capture of submitted preferences.
type prefsTestBeacon struct {
	beacon.BeaconNode
	dependentRoot phase0.Root
	// dependentRootErr makes the fresh dependent-root fetch fail; lastDependentRoot is what the client then
	// still remembers for the epoch (zero: nothing).
	dependentRootErr      error
	lastDependentRoot     phase0.Root
	submitted             [][]*gloas.SignedProposerPreferences
	submittedBuilderPrefs [][]*gloas.BuilderPreferencesEntry
}

func (b *prefsTestBeacon) ProposerDutiesDependentRoot(context.Context, phase0.Epoch) (phase0.Root, error) {
	if b.dependentRootErr != nil {
		return phase0.Root{}, b.dependentRootErr
	}
	return b.dependentRoot, nil
}

func (b *prefsTestBeacon) LastProposerDutiesDependentRoot(phase0.Epoch) (phase0.Root, bool) {
	return b.lastDependentRoot, b.lastDependentRoot != phase0.Root{}
}

func (b *prefsTestBeacon) SubmitProposerPreferences(_ context.Context, prefs []*gloas.SignedProposerPreferences) error {
	b.submitted = append(b.submitted, prefs)
	return nil
}

func (b *prefsTestBeacon) SubmitBuilderPreferences(_ context.Context, prefs []*gloas.BuilderPreferencesEntry) error {
	b.submittedBuilderPrefs = append(b.submittedBuilderPrefs, prefs)
	return nil
}

func TestNewProposerPreferencesRunner_RequiresSingleShare(t *testing.T) {
	_, err := NewProposerPreferencesRunner(ProposerPreferencesRunnerOptions{})
	require.Error(t, err)

	r, err := NewProposerPreferencesRunner(ProposerPreferencesRunnerOptions{
		BaseRunnerOptions: BaseRunnerOptions{
			Share: map[phase0.ValidatorIndex]*spectypes.Share{0: {}},
		},
	})
	require.NoError(t, err)
	require.Equal(t, spectypes.RoleProposerPreferences, r.(*ProposerPreferencesRunner).RunnerRoleType)
}

// Regression for the monotonic ShouldProcessNonBeaconDuty reject (runner.go): a validator can hold
// several lookahead proposal slots at once, and a HIGHER slot started first must not cause a
// subsequently started LOWER slot to be dropped. The dispatcher gives each slot its own sub-runner.
func TestProposerPreferencesRunner_ConcurrentSlotsTracked(t *testing.T) {
	netCfg := networkconfig.TestNetwork
	opts := ProposerPreferencesRunnerOptions{
		BaseRunnerOptions: BaseRunnerOptions{
			NetworkConfig: netCfg,
			Share:         map[phase0.ValidatorIndex]*spectypes.Share{0: {ValidatorIndex: 0}},
		},
		FeeRecipientProvider: errFeeRecipientProvider{}, // executeDuty fails fast; we assert the per-slot dispatch
	}
	r, err := NewProposerPreferencesRunner(opts)
	require.NoError(t, err)
	disp := r.(*ProposerPreferencesRunner)

	// Decreasing order is the exact case that broke the single runner: the higher slot, started first,
	// made the base runner reject the lower one as "already passed".
	current := netCfg.EstimatedCurrentSlot()
	for _, slot := range []phase0.Slot{current + 20, current + 10} {
		duty := &spectypes.ValidatorDuty{Type: spectypes.BNRoleProposerPreferences, ValidatorIndex: 0, Slot: slot}
		require.NoError(t, disp.StartNewDuty(context.Background(), zap.NewNop(), duty, 1))
	}

	require.Len(t, disp.bySlot, 2)               // both slots tracked, neither overwrote/rejected the other
	require.Contains(t, disp.bySlot, current+10) // the lower slot, started second, survived

	// No single current slot: the dispatcher is a MultiSlotRunner, which keeps the consumer's stale-message
	// floor off it — or the lower slot's partials would be purged when the higher slot's duty starts.
	_, hasSlot := disp.CurrentDutySlot()
	require.False(t, hasSlot)
}

// Once a validator's only started preference reaches quorum, the dispatcher reports no running duty while the
// slot's sub-runner stays assigned and its request-auth path (no succeeded-gate) still accepts partials. The
// queue consumer must therefore not hold the dispatcher's partials while it looks idle — which is what the
// MultiSlotRunner marker tells it (TestConsumeQueue_MultiSlotRunnerIsNotHeldWhileIdle).
func TestProposerPreferencesRunner_IdleAfterPreferenceSuccessKeepsSubRunner(t *testing.T) {
	sub := &proposerPreferencesSlotRunner{BaseRunner: &BaseRunner{RunnerRoleType: spectypes.RoleProposerPreferences}}
	sub.State = NewRunnerState(3, &spectypes.ValidatorDuty{Type: spectypes.BNRoleProposerPreferences, Slot: 100})
	sub.markDutySucceeded() // the preference reached quorum and was submitted

	dispatcher := &ProposerPreferencesRunner{
		BaseRunner: &BaseRunner{RunnerRoleType: spectypes.RoleProposerPreferences},
		bySlot:     map[phase0.Slot]*proposerPreferencesSlotRunner{100: sub},
	}

	require.True(t, sub.hasDutyAssigned(), "the sub-runner stays assigned: its request-auth path still accepts partials")
	require.False(t, dispatcher.HasRunningDuty(), "yet the dispatcher reports no running duty")
	_, multiSlot := Runner(dispatcher).(MultiSlotRunner)
	require.True(t, multiSlot, "so it declares itself multi-slot, and the consumer keeps its idle hold off it")
}

// evictPastSlots drops sub-runners whose proposal slot has already passed.
func TestProposerPreferencesRunner_evictPastSlots(t *testing.T) {
	netCfg := networkconfig.TestNetwork
	r, err := NewProposerPreferencesRunner(ProposerPreferencesRunnerOptions{
		BaseRunnerOptions: BaseRunnerOptions{
			NetworkConfig: netCfg,
			Share:         map[phase0.ValidatorIndex]*spectypes.Share{0: {}},
		},
	})
	require.NoError(t, err)
	disp := r.(*ProposerPreferencesRunner)

	current := netCfg.EstimatedCurrentSlot()
	disp.bySlot[current-1] = newProposerPreferencesSlotRunner(disp.opts, disp.builders)
	disp.bySlot[current+10] = newProposerPreferencesSlotRunner(disp.opts, disp.builders)

	disp.evictPastSlots()

	require.NotContains(t, disp.bySlot, current-1)
	require.Contains(t, disp.bySlot, current+10)
}

// A partial for a slot with no sub-runner is stashed, which is no error: StartNewDuty replays it once the
// slot's duty starts here, so the queue has nothing to retry or report as dropped.
func TestProposerPreferencesRunner_ProcessPreConsensus_unknownSlot(t *testing.T) {
	r, err := NewProposerPreferencesRunner(ProposerPreferencesRunnerOptions{
		BaseRunnerOptions: BaseRunnerOptions{Share: map[phase0.ValidatorIndex]*spectypes.Share{0: {Committee: make([]*spectypes.ShareMember, 4)}}},
	})
	require.NoError(t, err)

	require.NoError(t, r.ProcessPreConsensus(context.Background(), zap.NewNop(), &spectypes.PartialSignatureMessages{
		Type:     spectypes.ProposerPreferencesPartialSig,
		Slot:     999,
		Messages: []*spectypes.PartialSignatureMessage{{Signer: 1, SigningRoot: [32]byte{0xaa}}},
	}))
	require.Len(t, r.(*ProposerPreferencesRunner).pending[999], 1)
}

// A partial of any type other than the §5 duty's two is rejected with the spec's code, before it is
// stashed or routed to the slot's sub-runner.
func TestProposerPreferencesRunner_ProcessPreConsensus_unexpectedType(t *testing.T) {
	netCfg := networkconfig.TestNetwork
	r, err := NewProposerPreferencesRunner(ProposerPreferencesRunnerOptions{
		BaseRunnerOptions: BaseRunnerOptions{
			NetworkConfig: netCfg,
			Share:         map[phase0.ValidatorIndex]*spectypes.Share{0: {Committee: make([]*spectypes.ShareMember, 4)}},
		},
	})
	require.NoError(t, err)
	disp := r.(*ProposerPreferencesRunner)

	slot := netCfg.EstimatedCurrentSlot() + 10
	disp.bySlot[slot] = newProposerPreferencesSlotRunner(disp.opts, disp.builders)

	err = r.ProcessPreConsensus(context.Background(), zap.NewNop(), &spectypes.PartialSignatureMessages{
		Type:     spectypes.PTCAttesterPartialSig,
		Slot:     slot,
		Messages: []*spectypes.PartialSignatureMessage{{Signer: 1, SigningRoot: [32]byte{0xaa}}},
	})
	requireSpecCode(t, err, spectypes.ProposerPreferencesUnexpectedPartialSigTypeErrorCode)
	require.Empty(t, disp.pending[slot])
}

// stashPending dedups by (signer, signing root), caps a slot's stash at committee size times the
// per-signer distinct-root cap, and evictPastSlots prunes stashed slots alongside sub-runners.
func TestProposerPreferencesRunner_stashPending(t *testing.T) {
	netCfg := networkconfig.TestNetwork
	committee := make([]*spectypes.ShareMember, 2) // stash cap = 2 * maxPendingRootsPerSigner
	r, err := NewProposerPreferencesRunner(ProposerPreferencesRunnerOptions{
		BaseRunnerOptions: BaseRunnerOptions{
			NetworkConfig: netCfg,
			Share:         map[phase0.ValidatorIndex]*spectypes.Share{0: {Committee: committee}},
		},
	})
	require.NoError(t, err)
	disp := r.(*ProposerPreferencesRunner)

	slot := netCfg.EstimatedCurrentSlot() + 10
	msg := func(signer spectypes.OperatorID, root byte) *spectypes.PartialSignatureMessages {
		return &spectypes.PartialSignatureMessages{
			Type:     spectypes.ProposerPreferencesPartialSig,
			Slot:     slot,
			Messages: []*spectypes.PartialSignatureMessage{{Signer: signer, SigningRoot: [32]byte{root}}},
		}
	}

	disp.stashPending(msg(1, 0xaa))
	disp.stashPending(msg(1, 0xaa)) // duplicate (signer, root): skipped
	disp.stashPending(msg(2, 0xaa)) // same root, another signer: kept
	disp.stashPending(msg(1, 0xbb)) // same signer, another root: kept
	require.Len(t, disp.pending[slot], 3)

	for i := range 2*maxPendingRootsPerSigner + 8 { // well beyond the cap
		disp.stashPending(msg(spectypes.OperatorID(10+i), 0xcc))
	}
	require.Len(t, disp.pending[slot], 2*maxPendingRootsPerSigner)

	disp.pending[netCfg.EstimatedCurrentSlot()-1] = disp.pending[slot] // a stale slot
	disp.evictPastSlots()
	require.NotContains(t, disp.pending, netCfg.EstimatedCurrentSlot()-1)
	require.Contains(t, disp.pending, slot)
}

// End-to-end §5 convergence across emission skew: operators broadcast their partial exactly once, at
// their own emission tick, so peers' partials can precede the local duty (or a replacement of it).
// The dispatcher stashes every partial and replays it into a (re)started sub-runner so quorum still
// forms; an unchanged re-emission after a successful submit concludes idempotently (no duplicate
// broadcast or submit); a dependent_root change re-emits and awaits fresh partials.
func TestProposerPreferencesRunner_stashReplayConvergence(t *testing.T) {
	keySet := spectestingutils.Testing4SharesSet()
	share := spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex)
	cfg := cloneTestNetworkConfig()
	const quorum = 3
	const gasLimit = 36_000_000

	bn := &prefsTestBeacon{BeaconNode: protocoltesting.NewTestingBeaconNodeWrapped(), dependentRoot: phase0.Root{0xaa}}
	network := protocoltesting.NewTestingNetwork(1, keySet.OperatorKeys[1])
	feeRecipient := bellatrix.ExecutionAddress{0xfe}

	runnerIface, err := NewProposerPreferencesRunner(ProposerPreferencesRunnerOptions{
		BaseRunnerOptions: BaseRunnerOptions{
			NetworkConfig:  cfg,
			Share:          map[phase0.ValidatorIndex]*spectypes.Share{share.ValidatorIndex: share},
			Beacon:         bn,
			Network:        network,
			Signer:         ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager()),
			OperatorSigner: spectestingutils.NewOperatorSigner(keySet, 1),
		},
		FeeRecipientProvider: fixedFeeRecipientProvider{addr: feeRecipient},
		GasLimit:             gasLimit,
	})
	require.NoError(t, err)
	disp := runnerIface.(*ProposerPreferencesRunner)

	proposalSlot := cfg.EstimatedCurrentSlot() + 5
	duty := &spectypes.ValidatorDuty{
		Type:           spectypes.BNRoleProposerPreferences,
		PubKey:         spectestingutils.TestingValidatorPubKey,
		Slot:           proposalSlot,
		ValidatorIndex: share.ValidatorIndex,
	}

	// peerPartial signs the preference every operator is expected to converge on, as peer opID.
	peerPartial := func(t *testing.T, opID spectypes.OperatorID, dependentRoot phase0.Root) *spectypes.PartialSignatureMessages {
		t.Helper()
		prefs := &gloas.ProposerPreferences{
			DependentRoot:  dependentRoot,
			ProposalSlot:   proposalSlot,
			ValidatorIndex: share.ValidatorIndex,
			FeeRecipient:   feeRecipient,
			TargetGasLimit: gasLimit,
		}
		domain, err := bn.DomainData(context.Background(), cfg.EstimatedEpochAtSlot(proposalSlot), phase0.DomainType(spectypes.DomainProposerPreferences))
		require.NoError(t, err)
		root, err := spectypes.ComputeETHSigningRoot(prefs, domain)
		require.NoError(t, err)
		sig := keySet.Shares[opID].SignByte(root[:])
		return &spectypes.PartialSignatureMessages{
			Type: spectypes.ProposerPreferencesPartialSig,
			Slot: proposalSlot,
			Messages: []*spectypes.PartialSignatureMessage{{
				PartialSignature: sig.Serialize(),
				SigningRoot:      root,
				Signer:           opID,
				ValidatorIndex:   share.ValidatorIndex,
			}},
		}
	}

	ctx := context.Background()
	logger := zap.NewNop()

	// Peers 2..4 emitted before us: their one-shot partials arrive with no local duty and are stashed.
	for _, op := range []spectypes.OperatorID{2, 3, 4} {
		require.NoError(t, disp.ProcessPreConsensus(ctx, logger, peerPartial(t, op, bn.dependentRoot)))
	}

	// Our own (late) emission: the replay of the stashed partials completes quorum and submits.
	require.NoError(t, disp.StartNewDuty(ctx, logger, duty, quorum))
	require.Len(t, bn.submitted, 1, "stashed partials must be replayed to quorum on duty start")
	require.Len(t, network.BroadcastedMsgs, 1, "own partial broadcast exactly once")
	require.Equal(t, phase0.Root{0xaa}, bn.submitted[0][0].Message.DependentRoot)

	// An unchanged re-emission (e.g. an indices-change re-emit under the same root) is idempotent.
	require.NoError(t, disp.StartNewDuty(ctx, logger, duty, quorum))
	require.Len(t, bn.submitted, 1, "unchanged re-emission must not resubmit")
	require.Len(t, network.BroadcastedMsgs, 1, "unchanged re-emission must not re-broadcast")

	// A dependent_root change re-emits: fresh broadcast; the stale-root stashed partials fail
	// verification against the new frozen preference and must not complete its quorum.
	bn.dependentRoot = phase0.Root{0xbb}
	require.NoError(t, disp.StartNewDuty(ctx, logger, duty, quorum))
	require.Len(t, network.BroadcastedMsgs, 2, "root change must re-broadcast a fresh partial")
	require.Len(t, bn.submitted, 1, "stale-root partials must not complete the new quorum")

	// A re-emission while the duty is in flight (broadcast, quorum still pending) with an unchanged
	// root must not re-broadcast — peers already hold the identical partial and would IGNORE the repeat
	// (issue #2934) — and the duty must keep converging on the carried-over broadcast state.
	require.NoError(t, disp.StartNewDuty(ctx, logger, duty, quorum))
	require.Len(t, network.BroadcastedMsgs, 2, "in-flight re-emission with an unchanged root must not re-broadcast")
	require.Len(t, bn.submitted, 1)

	// The peers' new-root partials arrive live; quorum re-forms and the updated preference submits.
	for _, op := range []spectypes.OperatorID{2, 3, 4} {
		require.NoError(t, disp.ProcessPreConsensus(ctx, logger, peerPartial(t, op, bn.dependentRoot)))
	}
	require.Len(t, bn.submitted, 2, "the re-emitted preference must submit once its quorum forms")
	require.Equal(t, phase0.Root{0xbb}, bn.submitted[1][0].Message.DependentRoot)
}

// switchableFeeRecipientProvider returns addr until fail is set.
type switchableFeeRecipientProvider struct {
	addr bellatrix.ExecutionAddress
	fail bool
}

func (p *switchableFeeRecipientProvider) GetFeeRecipient(spectypes.ValidatorPK) (bellatrix.ExecutionAddress, error) {
	if p.fail {
		return bellatrix.ExecutionAddress{}, fmt.Errorf("no fee recipient")
	}
	return p.addr, nil
}

// A re-emission that can't rebuild its preference keeps converging on the one already broadcast: the stash
// replays the partials gathered for it into the replacement, and the next one completes the quorum.
func TestProposerPreferencesRunner_reemissionThatCannotRebuildKeepsBroadcastPreference(t *testing.T) {
	keySet := spectestingutils.Testing4SharesSet()
	share := spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex)
	cfg := cloneTestNetworkConfig()
	const quorum = 3
	const gasLimit = 36_000_000

	bn := &prefsTestBeacon{BeaconNode: protocoltesting.NewTestingBeaconNodeWrapped(), dependentRoot: phase0.Root{0xaa}}
	network := protocoltesting.NewTestingNetwork(1, keySet.OperatorKeys[1])
	feeRecipients := &switchableFeeRecipientProvider{addr: bellatrix.ExecutionAddress{0xfe}}

	runnerIface, err := NewProposerPreferencesRunner(ProposerPreferencesRunnerOptions{
		BaseRunnerOptions: BaseRunnerOptions{
			NetworkConfig:  cfg,
			Share:          map[phase0.ValidatorIndex]*spectypes.Share{share.ValidatorIndex: share},
			Beacon:         bn,
			Network:        network,
			Signer:         ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager()),
			OperatorSigner: spectestingutils.NewOperatorSigner(keySet, 1),
		},
		FeeRecipientProvider: feeRecipients,
		GasLimit:             gasLimit,
	})
	require.NoError(t, err)
	disp := runnerIface.(*ProposerPreferencesRunner)

	proposalSlot := cfg.EstimatedCurrentSlot() + 5
	duty := &spectypes.ValidatorDuty{
		Type:           spectypes.BNRoleProposerPreferences,
		PubKey:         spectestingutils.TestingValidatorPubKey,
		Slot:           proposalSlot,
		ValidatorIndex: share.ValidatorIndex,
	}

	peerPartial := func(t *testing.T, opID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
		t.Helper()
		prefs := &gloas.ProposerPreferences{
			DependentRoot:  bn.dependentRoot,
			ProposalSlot:   proposalSlot,
			ValidatorIndex: share.ValidatorIndex,
			FeeRecipient:   feeRecipients.addr,
			TargetGasLimit: gasLimit,
		}
		domain, err := bn.DomainData(context.Background(), cfg.EstimatedEpochAtSlot(proposalSlot), phase0.DomainType(spectypes.DomainProposerPreferences))
		require.NoError(t, err)
		root, err := spectypes.ComputeETHSigningRoot(prefs, domain)
		require.NoError(t, err)
		sig := keySet.Shares[opID].SignByte(root[:])
		return &spectypes.PartialSignatureMessages{
			Type: spectypes.ProposerPreferencesPartialSig,
			Slot: proposalSlot,
			Messages: []*spectypes.PartialSignatureMessage{{
				PartialSignature: sig.Serialize(),
				SigningRoot:      root,
				Signer:           opID,
				ValidatorIndex:   share.ValidatorIndex,
			}},
		}
	}

	ctx := context.Background()
	logger := zap.NewNop()

	// Our emission broadcasts the preference, and two peers' partials follow: one short of quorum.
	require.NoError(t, disp.StartNewDuty(ctx, logger, duty, quorum))
	for _, op := range []spectypes.OperatorID{2, 3} {
		require.NoError(t, disp.ProcessPreConsensus(ctx, logger, peerPartial(t, op)))
	}
	require.Empty(t, bn.submitted)

	// The re-emission can't build a preference, so it keeps the broadcast one without re-signing it.
	feeRecipients.fail = true
	require.NoError(t, disp.StartNewDuty(ctx, logger, duty, quorum))
	concluded := observeDutyConclusion(disp.bySlot[proposalSlot].BaseRunner)
	require.Len(t, network.BroadcastedMsgs, 1, "the kept preference is not re-broadcast")

	// The third peer's partial completes the quorum on the replayed two.
	require.NoError(t, disp.ProcessPreConsensus(ctx, logger, peerPartial(t, 4)))
	require.Len(t, bn.submitted, 1)
	require.Equal(t, phase0.Root{0xaa}, bn.submitted[0][0].Message.DependentRoot)
	requireConcluded(t, concluded, dutyOutcomeSucceeded)
}

// Proposer preferences have no consensus or post-consensus phase; those entry points must reject.
func TestProposerPreferencesRunner_NoConsensusPhases(t *testing.T) {
	r := &ProposerPreferencesRunner{}
	require.Error(t, r.ProcessConsensus(context.Background(), zap.NewNop(), nil))
	require.Error(t, r.ProcessPostConsensus(context.Background(), zap.NewNop(), nil))
}

// The runner validates and aggregates incoming partial signatures against its own frozen preference:
// there is no expected root before executeDuty has built and frozen one, and afterwards it is exactly
// that preference's root under DomainProposerPreferences.
func TestProposerPreferencesSlotRunner_ExpectedPreConsensusRootsAndDomain(t *testing.T) {
	r := &proposerPreferencesSlotRunner{}

	_, _, err := r.expectedPreConsensusRootsAndDomain()
	require.Error(t, err)

	prefs := &gloas.ProposerPreferences{DependentRoot: phase0.Root{0x01}, ProposalSlot: 5, ValidatorIndex: 7}
	r.proposerPreferences = prefs
	roots, domain, err := r.expectedPreConsensusRootsAndDomain()
	require.NoError(t, err)
	require.Equal(t, []spectypes.HashRoot{prefs}, roots)
	require.Equal(t, phase0.DomainType(spectypes.DomainProposerPreferences), domain)
}

// A dependent-root fetch that fails at emission does not fail the one-shot §5 duty while the beacon client
// still remembers the root the scheduler emitted under: the preference is built under that root and
// broadcast. With nothing remembered the duty fails as before, and no preference goes out.
func TestProposerPreferencesSlotRunner_buildFallsBackToLastDependentRoot(t *testing.T) {
	keySet := spectestingutils.Testing4SharesSet()
	share := spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex)
	cfg := cloneTestNetworkConfig()
	proposalSlot := cfg.EstimatedCurrentSlot() + 5
	duty := &spectypes.ValidatorDuty{
		Type:           spectypes.BNRoleProposerPreferences,
		PubKey:         spectestingutils.TestingValidatorPubKey,
		Slot:           proposalSlot,
		ValidatorIndex: share.ValidatorIndex,
	}

	newDispatcher := func(t *testing.T, bn *prefsTestBeacon) (*ProposerPreferencesRunner, *protocoltesting.TestingNetwork) {
		t.Helper()
		network := protocoltesting.NewTestingNetwork(1, keySet.OperatorKeys[1])
		runnerIface, err := NewProposerPreferencesRunner(ProposerPreferencesRunnerOptions{
			BaseRunnerOptions: BaseRunnerOptions{
				NetworkConfig:  cfg,
				Share:          map[phase0.ValidatorIndex]*spectypes.Share{share.ValidatorIndex: share},
				Beacon:         bn,
				Network:        network,
				Signer:         ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager()),
				OperatorSigner: spectestingutils.NewOperatorSigner(keySet, 1),
			},
			FeeRecipientProvider: fixedFeeRecipientProvider{addr: bellatrix.ExecutionAddress{0xfe}},
			GasLimit:             36_000_000,
		})
		require.NoError(t, err)
		return runnerIface.(*ProposerPreferencesRunner), network
	}

	t.Run("a remembered root: built and broadcast under it", func(t *testing.T) {
		bn := &prefsTestBeacon{
			BeaconNode:        protocoltesting.NewTestingBeaconNodeWrapped(),
			dependentRootErr:  fmt.Errorf("beacon node down"),
			lastDependentRoot: phase0.Root{0xaa},
		}
		disp, network := newDispatcher(t, bn)
		require.NoError(t, disp.StartNewDuty(context.Background(), zap.NewNop(), duty, 3))

		sub := disp.bySlot[proposalSlot]
		require.NotNil(t, sub.proposerPreferences, "the preference was built")
		require.Equal(t, phase0.Root{0xaa}, sub.proposerPreferences.DependentRoot, "under the remembered root")
		require.Equal(t, 1, broadcastPartialSigTypes(t, network.BroadcastedMsgs)[spectypes.ProposerPreferencesPartialSig], "and broadcast")
	})

	t.Run("nothing remembered: the duty fails and nothing goes out", func(t *testing.T) {
		bn := &prefsTestBeacon{BeaconNode: protocoltesting.NewTestingBeaconNodeWrapped(), dependentRootErr: fmt.Errorf("beacon node down")}
		disp, network := newDispatcher(t, bn)
		require.NoError(t, disp.StartNewDuty(context.Background(), zap.NewNop(), duty, 3), "a build failure is recorded as a failed duty, not returned")

		require.Nil(t, disp.bySlot[proposalSlot].proposerPreferences)
		require.Empty(t, network.BroadcastedMsgs)
	})
}
