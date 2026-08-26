package runner

import (
	"context"
	"fmt"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

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
	dependentRoot         phase0.Root
	submitted             [][]*gloas.SignedProposerPreferences
	submittedBuilderPrefs [][]*gloas.BuilderPreferencesEntry
}

func (b *prefsTestBeacon) ProposerDutiesDependentRoot(context.Context, phase0.Epoch) (phase0.Root, error) {
	return b.dependentRoot, nil
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

// An incoming partial signature for a slot with no sub-runner is retryable, not a hard error.
func TestProposerPreferencesRunner_ProcessPreConsensus_unknownSlot(t *testing.T) {
	r, err := NewProposerPreferencesRunner(ProposerPreferencesRunnerOptions{
		BaseRunnerOptions: BaseRunnerOptions{Share: map[phase0.ValidatorIndex]*spectypes.Share{0: {}}},
	})
	require.NoError(t, err)

	err = r.ProcessPreConsensus(context.Background(), zap.NewNop(), &spectypes.PartialSignatureMessages{Slot: 999})
	require.Error(t, err)
	require.True(t, IsRetryable(err))
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
		err := disp.ProcessPreConsensus(ctx, logger, peerPartial(t, op, bn.dependentRoot))
		require.Error(t, err)
		require.True(t, IsRetryable(err))
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
	// root must not re-broadcast — peers would reject the identical partial as a same-peer duplicate
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
	require.Equal(t, []ssz.HashRoot{prefs}, roots)
	require.Equal(t, phase0.DomainType(spectypes.DomainProposerPreferences), domain)
}
