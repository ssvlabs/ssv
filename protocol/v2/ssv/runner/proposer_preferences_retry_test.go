package runner

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"

	"github.com/ssvlabs/ssv/networkconfig"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

// flakyPrefsBeacon fails its first failures preference submits (all of them when failures is negative). It
// locks around its counters: the retry submits from its own goroutine.
type flakyPrefsBeacon struct {
	*prefsTestBeacon

	mu       sync.Mutex
	failures int
	attempts int
	landed   []*gloas.SignedProposerPreferences
}

func (b *flakyPrefsBeacon) SubmitProposerPreferences(_ context.Context, prefs []*gloas.SignedProposerPreferences) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.attempts++
	if b.failures != 0 {
		if b.failures > 0 {
			b.failures--
		}
		return errors.New("beacon node unavailable")
	}
	b.landed = append(b.landed, prefs...)
	return nil
}

func (b *flakyPrefsBeacon) counts() (attempts, landed int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.attempts, len(b.landed)
}

// prefsSubmitRetryEnv is a proposer-preferences dispatcher for operator 1 of a 4-operator cluster, whose
// preference submits go to a flakyPrefsBeacon.
type prefsSubmitRetryEnv struct {
	disp         *ProposerPreferencesRunner
	bn           *flakyPrefsBeacon
	duty         *spectypes.ValidatorDuty
	proposalSlot phase0.Slot
	peerPartial  func(t *testing.T, opID spectypes.OperatorID) *spectypes.PartialSignatureMessages
}

const prefsRetryQuorum = 3

func newPrefsSubmitRetryEnv(t *testing.T, cfg *networkconfig.Network, proposalSlot phase0.Slot, failures int) *prefsSubmitRetryEnv {
	t.Helper()

	// Retry fast; read on the message loop (this goroutine) when a retry starts.
	prevDelay := preferencesSubmitRetryDelay
	preferencesSubmitRetryDelay = 10 * time.Millisecond
	t.Cleanup(func() { preferencesSubmitRetryDelay = prevDelay })

	keySet := spectestingutils.Testing4SharesSet()
	share := spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex)
	const gasLimit = 36_000_000
	feeRecipient := bellatrix.ExecutionAddress{0xfe}

	bn := &flakyPrefsBeacon{
		prefsTestBeacon: &prefsTestBeacon{BeaconNode: protocoltesting.NewTestingBeaconNodeWrapped(), dependentRoot: phase0.Root{0xaa}},
		failures:        failures,
	}
	runnerIface, err := NewProposerPreferencesRunner(ProposerPreferencesRunnerOptions{
		BaseRunnerOptions: BaseRunnerOptions{
			NetworkConfig:  cfg,
			Share:          map[phase0.ValidatorIndex]*spectypes.Share{share.ValidatorIndex: share},
			Beacon:         bn,
			Network:        protocoltesting.NewTestingNetwork(1, keySet.OperatorKeys[1]),
			Signer:         ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager()),
			OperatorSigner: spectestingutils.NewOperatorSigner(keySet, 1),
		},
		FeeRecipientProvider: fixedFeeRecipientProvider{addr: feeRecipient},
		GasLimit:             gasLimit,
	})
	require.NoError(t, err)

	peerPartial := func(t *testing.T, opID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
		t.Helper()
		prefs := &gloas.ProposerPreferences{
			DependentRoot:  bn.dependentRoot,
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

	return &prefsSubmitRetryEnv{
		disp: runnerIface.(*ProposerPreferencesRunner),
		bn:   bn,
		duty: &spectypes.ValidatorDuty{
			Type:           spectypes.BNRoleProposerPreferences,
			PubKey:         spectestingutils.TestingValidatorPubKey,
			Slot:           proposalSlot,
			ValidatorIndex: share.ValidatorIndex,
		},
		proposalSlot: proposalSlot,
		peerPartial:  peerPartial,
	}
}

// startToFailedSubmit starts the duty, then completes its quorum with three peers' partials (the operator's own
// one comes back only through the network), so the first submit fails and the retry starts. It returns the
// channel observing the duty's conclusion.
func (e *prefsSubmitRetryEnv) startToFailedSubmit(t *testing.T) chan dutyConclusion {
	t.Helper()
	ctx, logger := t.Context(), zap.NewNop()

	require.NoError(t, e.disp.StartNewDuty(ctx, logger, e.duty, prefsRetryQuorum))
	concluded := observeDutyConclusion(e.disp.bySlot[e.proposalSlot].BaseRunner)
	for _, op := range []spectypes.OperatorID{2, 3, 4} {
		require.NoError(t, e.disp.ProcessPreConsensus(ctx, logger, e.peerPartial(t, op)), "a failed submit is retried, not returned")
	}
	attempts, _ := e.bn.counts()
	require.Equal(t, 1, attempts, "the quorum submits once in place")
	return concluded
}

func awaitConclusion(t *testing.T, concluded chan dutyConclusion, want dutyOutcome) {
	t.Helper()
	select {
	case c := <-concluded:
		require.Equal(t, want, c.outcome, "reason: %v", c.reason)
	case <-time.After(5 * time.Second):
		t.Fatalf("the duty has not concluded, want %s", want)
	}
}

// requireNoMoreAttempts checks that no submit attempt follows the ones made so far.
func (e *prefsSubmitRetryEnv) requireNoMoreAttempts(t *testing.T) {
	t.Helper()
	before, _ := e.bn.counts()
	time.Sleep(100 * time.Millisecond)
	after, _ := e.bn.counts()
	require.Equal(t, before, after, "the retry must have stopped")
}

// A submit the beacon node fails is retried in the background, with the same signed preference, until it lands,
// and the duty then concludes succeeded. An unchanged re-emission afterwards knows it was submitted.
func TestProposerPreferencesRunner_retriesFailedSubmit(t *testing.T) {
	cfg := cloneTestNetworkConfig()
	env := newPrefsSubmitRetryEnv(t, cfg, cfg.EstimatedCurrentSlot()+5, 2)

	concluded := env.startToFailedSubmit(t)
	awaitConclusion(t, concluded, dutyOutcomeSucceeded)

	attempts, landed := env.bn.counts()
	require.Equal(t, 3, attempts, "two failed submits, then the one that lands")
	require.Equal(t, 1, landed)
	require.Equal(t, phase0.Root{0xaa}, env.bn.landed[0].Message.DependentRoot)
	env.requireNoMoreAttempts(t)

	require.NoError(t, env.disp.StartNewDuty(t.Context(), zap.NewNop(), env.duty, prefsRetryQuorum))
	require.True(t, env.disp.bySlot[env.proposalSlot].hasDutySucceeded(), "an unchanged re-emission after the retried submit is not required")
	attempts, _ = env.bn.counts()
	require.Equal(t, 3, attempts, "an unchanged re-emission must not resubmit")
}

// A re-emission that replaces the slot runner stops its retry, which concludes the replaced duty as not required,
// as for any replaced running duty. Here a dependent_root change re-emits, so the retried preference has gone
// stale, and the stashed partials, signed over the old root, can't re-form a quorum for the replacement.
func TestProposerPreferencesRunner_reemissionStopsSubmitRetry(t *testing.T) {
	cfg := cloneTestNetworkConfig()
	env := newPrefsSubmitRetryEnv(t, cfg, cfg.EstimatedCurrentSlot()+5, -1)

	concluded := env.startToFailedSubmit(t)
	env.bn.dependentRoot = phase0.Root{0xbb}
	require.NoError(t, env.disp.StartNewDuty(t.Context(), zap.NewNop(), env.duty, prefsRetryQuorum))
	awaitConclusion(t, concluded, dutyOutcomeNotRequired)
	env.requireNoMoreAttempts(t)
}

// The retry gives up when the proposal slot starts, since the preference is moot from then on. It leaves the
// conclusion to the duty's watcher, which reports the first submit failure noted for it.
func TestProposerPreferencesRunner_submitRetryStopsAtProposalSlot(t *testing.T) {
	cfg := cloneTestNetworkConfig()
	const proposalSlot = phase0.Slot(100)
	cfg.GenesisTime = time.Now().Add(-time.Duration(proposalSlot)*cfg.SlotDuration + 500*time.Millisecond)
	env := newPrefsSubmitRetryEnv(t, cfg, proposalSlot, -1)

	concluded := env.startToFailedSubmit(t)
	time.Sleep(time.Until(cfg.SlotStartTime(proposalSlot)))
	env.requireNoMoreAttempts(t)

	attempts, landed := env.bn.counts()
	require.Greater(t, attempts, 1, "the submit must have been retried before the proposal slot")
	require.Zero(t, landed)
	requireNotedFailure(t, env.disp.bySlot[proposalSlot].BaseRunner, concluded, "could not submit proposer preferences: beacon node unavailable")
}
