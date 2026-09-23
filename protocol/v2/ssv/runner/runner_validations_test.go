package runner

import (
	"context"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/testing/mocks"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

// A partial that reaches a runner before its duty started, or after the duty succeeded, is reported
// through the two duty-state sentinels and is not retried: the duty queue already holds such messages
// while no duty runs (see ValidatePreConsensusMsg). For every runner, pre- and post-consensus, the
// sentinel is reachable to errors.Is, the spec code to errors.As, and the error is not retryable.
func TestDutyStateSentinels_NotRetried(t *testing.T) {
	msgs := &spectypes.PartialSignatureMessages{
		Type:     spectypes.RandaoPartialSig,
		Slot:     1,
		Messages: []*spectypes.PartialSignatureMessage{{Signer: 1, ValidatorIndex: 1}},
	}
	ctx, logger := context.Background(), zap.NewNop()
	succeeded := func() *BaseRunner {
		state := NewRunnerState(3, &spectypes.ValidatorDuty{Type: spectypes.BNRoleProposer, Slot: 1})
		state.Succeeded = true
		return &BaseRunner{State: state}
	}

	runners := []struct {
		name    string
		process func(*BaseRunner) error
	}{
		{"proposer pre-consensus", func(b *BaseRunner) error {
			return (&ProposerRunner{BaseRunner: b}).ProcessPreConsensus(ctx, logger, msgs)
		}},
		{"proposer post-consensus", func(b *BaseRunner) error {
			return (&ProposerRunner{BaseRunner: b}).ProcessPostConsensus(ctx, logger, msgs)
		}},
		{"aggregator pre-consensus", func(b *BaseRunner) error {
			return (&AggregatorRunner{BaseRunner: b}).ProcessPreConsensus(ctx, logger, msgs)
		}},
		{"sync-committee contribution pre-consensus", func(b *BaseRunner) error {
			return (&SyncCommitteeAggregatorRunner{BaseRunner: b}).ProcessPreConsensus(ctx, logger, msgs)
		}},
		{"validator registration pre-consensus", func(b *BaseRunner) error {
			return (&ValidatorRegistrationRunner{BaseRunner: b}).ProcessPreConsensus(ctx, logger, msgs)
		}},
		{"voluntary exit pre-consensus", func(b *BaseRunner) error {
			return (&VoluntaryExitRunner{BaseRunner: b}).ProcessPreConsensus(ctx, logger, msgs)
		}},
		{"committee post-consensus", func(b *BaseRunner) error {
			return (&CommitteeRunner{BaseRunner: b}).ProcessPostConsensus(ctx, logger, msgs)
		}},
		{"PTC attester pre-consensus", func(b *BaseRunner) error {
			return (&PTCAttesterRunner{BaseRunner: b}).ProcessPreConsensus(ctx, logger, msgs)
		}},
		{"proposer-preferences slot pre-consensus", func(b *BaseRunner) error {
			return (&proposerPreferencesSlotRunner{BaseRunner: b}).ProcessPreConsensus(ctx, logger, msgs)
		}},
	}
	for _, r := range runners {
		t.Run(r.name, func(t *testing.T) {
			err := r.process(&BaseRunner{})
			require.ErrorIs(t, err, ErrNoDutyAssigned)
			requireSpecCode(t, err, spectypes.NoRunningDutyErrorCode)
			require.False(t, IsRetryable(err))

			err = r.process(succeeded())
			require.ErrorIs(t, err, ErrRunningDutySucceeded)
			requireSpecCode(t, err, spectypes.NoRunningDutyErrorCode)
			require.False(t, IsRetryable(err))
		})
	}
}

func requireSpecCode(t *testing.T, err error, code int) {
	t.Helper()
	var specErr *spectypes.Error
	require.ErrorAs(t, err, &specErr)
	require.Equal(t, code, specErr.Code)
}

// After a failed reconstruct the bad shares are dropped. A root that fell below quorum is recoverable, one still
// at quorum is retried on the remaining shares, and with no bad share to drop the failure is terminal.
func TestReconstructQuorumSig(t *testing.T) {
	t.Parallel()

	keySet := spectestingutils.Testing4SharesSet()
	share := spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex)
	root := [32]byte{0x42}
	// withShares is a runner whose post-consensus container holds valid shares of root from the good
	// operators and, from the bad ones, shares over another root, which fail verification.
	withShares := func(good, bad []spectypes.OperatorID) (*BaseRunner, *ssv.PartialSigContainer) {
		b := &BaseRunner{State: NewRunnerState(keySet.Threshold, &spectypes.ValidatorDuty{})}
		container := b.State.PostConsensusContainer
		add := func(op spectypes.OperatorID, signed [32]byte) {
			container.AddSignature(&spectypes.PartialSignatureMessage{
				PartialSignature: keySet.Shares[op].SignByte(signed[:]).Serialize(),
				SigningRoot:      root,
				Signer:           op,
				ValidatorIndex:   share.ValidatorIndex,
			})
		}
		for _, op := range bad {
			add(op, [32]byte{0xff})
		}
		for _, op := range good {
			add(op, root)
		}
		return b, container
	}
	shares := func(container *ssv.PartialSigContainer) int {
		return len(container.GetSignatures(share.ValidatorIndex, root))
	}

	t.Run("still at quorum after the drop: retried on the remaining shares", func(t *testing.T) {
		b, container := withShares([]spectypes.OperatorID{2, 3, 4}, []spectypes.OperatorID{1})
		sig, err := b.reconstructQuorumSig(container, root, share, "post-consensus")
		require.NoError(t, err)
		require.NotEqual(t, phase0.BLSSignature{}, sig)
		require.Equal(t, 3, shares(container))
	})

	t.Run("below quorum after the drop: recoverable", func(t *testing.T) {
		b, container := withShares([]spectypes.OperatorID{2, 3}, []spectypes.OperatorID{1})
		_, err := b.reconstructQuorumSig(container, root, share, "post-consensus")
		require.ErrorContains(t, err, "got post-consensus quorum but it has invalid signatures")
		require.True(t, isRecoverableReconstructError(err))
		require.Equal(t, 2, shares(container))
	})

	t.Run("no bad share to drop: terminal", func(t *testing.T) {
		b, container := withShares([]spectypes.OperatorID{1, 2, 3}, nil)
		// Every share verifies, but they combine to another validator's key than the share's.
		var other bls.SecretKey
		other.SetByCSPRNG()
		otherShare := *share
		otherShare.ValidatorPubKey = spectypes.ValidatorPK(other.GetPublicKey().Serialize())
		_, err := b.reconstructQuorumSig(container, root, &otherShare, "post-consensus")
		require.Error(t, err)
		require.False(t, isRecoverableReconstructError(err))
		require.Equal(t, 3, shares(container))
	})
}

// The runners with no consensus phase complete their duty on the pre-consensus quorum. A bad share in it
// doesn't fail the duty: the fallback drops it, an honest share brings the root back to quorum, and the duty
// concludes succeeded.
func TestProcessPreConsensusRecoversFromBadShare(t *testing.T) {
	t.Parallel()

	keySet := spectestingutils.Testing4SharesSet()
	share := spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex)
	options := func(cfg *networkconfig.Network, bn beacon.BeaconNode) BaseRunnerOptions {
		return BaseRunnerOptions{
			NetworkConfig:  cfg,
			Share:          map[phase0.ValidatorIndex]*spectypes.Share{share.ValidatorIndex: share},
			Beacon:         bn,
			Network:        protocoltesting.NewTestingNetwork(1, keySet.OperatorKeys[1]),
			Signer:         ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager()),
			OperatorSigner: spectestingutils.NewOperatorSigner(keySet, 1),
		}
	}
	start := func(t *testing.T, r Runner, duty spectypes.Duty) {
		t.Helper()
		require.NoError(t, r.StartNewDuty(t.Context(), zap.NewNop(), duty, keySet.Threshold))
	}

	tests := []struct {
		name    string
		msgType spectypes.PartialSigMsgType
		// setup starts a duty. It returns the runner that runs it, that runner's base, and a count of what it
		// submitted to the beacon node.
		setup func(t *testing.T) (Runner, *BaseRunner, func() int)
	}{
		{
			name:    "validator registration",
			msgType: spectypes.ValidatorRegistrationPartialSig,
			setup: func(t *testing.T) (Runner, *BaseRunner, func() int) {
				bn := protocoltesting.NewTestingBeaconNodeWrapped().(*protocoltesting.BeaconNodeWrapped)
				r, err := NewValidatorRegistrationRunner(ValidatorRegistrationRunnerOptions{
					BaseRunnerOptions:              options(networkconfig.TestNetwork, bn),
					ValidatorRegistrationSubmitter: mocks.NewValidatorRegistrationSubmitter(bn),
					FeeRecipientProvider:           &mocks.FeeRecipientProvider{},
					GasLimit:                       spectypes.DefaultGasLimit,
				})
				require.NoError(t, err)
				duty := spectestingutils.TestingValidatorRegistrationDuty
				start(t, r, &duty)
				return r, r.(*ValidatorRegistrationRunner).BaseRunner, func() int { return len(bn.GetBroadcastedRoots()) }
			},
		},
		{
			name:    "voluntary exit",
			msgType: spectypes.VoluntaryExitPartialSig,
			setup: func(t *testing.T) (Runner, *BaseRunner, func() int) {
				bn := protocoltesting.NewTestingBeaconNodeWrapped().(*protocoltesting.BeaconNodeWrapped)
				r, err := NewVoluntaryExitRunner(VoluntaryExitRunnerOptions{BaseRunnerOptions: options(networkconfig.TestNetwork, bn)})
				require.NoError(t, err)
				duty := spectestingutils.TestingVoluntaryExitDuty
				start(t, r, &duty)
				return r, r.(*VoluntaryExitRunner).BaseRunner, func() int { return len(bn.GetBroadcastedRoots()) }
			},
		},
		{
			name:    "PTC attester",
			msgType: spectypes.PTCAttesterPartialSig,
			setup: func(t *testing.T) (Runner, *BaseRunner, func() int) {
				duty := spectestingutils.TestingPTCAttesterDuty()
				bn := &ptcTestBeacon{
					BeaconNode: protocoltesting.NewTestingBeaconNodeWrapped(),
					data:       &gloas.PayloadAttestationData{BeaconBlockRoot: phase0.Root{0x01}, Slot: duty.Slot, PayloadPresent: true},
				}
				r, err := NewPTCAttesterRunner(PTCAttesterRunnerOptions{BaseRunnerOptions: options(networkconfig.TestNetwork, bn)})
				require.NoError(t, err)
				start(t, r, duty)
				return r, r.(*PTCAttesterRunner).BaseRunner, func() int { return len(bn.submitted) }
			},
		},
		{
			name:    "proposer preferences",
			msgType: spectypes.ProposerPreferencesPartialSig,
			setup: func(t *testing.T) (Runner, *BaseRunner, func() int) {
				cfg := cloneTestNetworkConfig()
				bn := &prefsTestBeacon{BeaconNode: protocoltesting.NewTestingBeaconNodeWrapped(), dependentRoot: phase0.Root{0xaa}}
				r, err := NewProposerPreferencesRunner(ProposerPreferencesRunnerOptions{
					BaseRunnerOptions:    options(cfg, bn),
					FeeRecipientProvider: fixedFeeRecipientProvider{addr: bellatrix.ExecutionAddress{0xfe}},
					GasLimit:             spectypes.DefaultGasLimit,
				})
				require.NoError(t, err)
				duty := &spectypes.ValidatorDuty{
					Type:           spectypes.BNRoleProposerPreferences,
					PubKey:         spectestingutils.TestingValidatorPubKey,
					Slot:           cfg.EstimatedCurrentSlot() + 5,
					ValidatorIndex: share.ValidatorIndex,
				}
				start(t, r, duty)
				// The duty runs on the proposal slot's sub-runner.
				sub := r.(*ProposerPreferencesRunner).bySlot[duty.Slot]
				return sub, sub.BaseRunner, func() int { return len(bn.submitted) }
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, logger := t.Context(), zap.NewNop()
			r, base, submitted := tt.setup(t)
			concluded := observeDutyConclusion(base)

			slot := base.State.CurrentDuty.DutySlot()
			objs, domainType, err := r.expectedPreConsensusRootsAndDomain()
			require.NoError(t, err)
			domain, err := r.GetBeaconNode().DomainData(ctx, base.NetworkConfig.EstimatedEpochAtSlot(slot), domainType)
			require.NoError(t, err)
			root, err := spectypes.ComputeETHSigningRoot(objs[0], domain)
			require.NoError(t, err)
			partial := func(op spectypes.OperatorID) *spectypes.PartialSignatureMessages {
				return &spectypes.PartialSignatureMessages{
					Type: tt.msgType,
					Slot: slot,
					Messages: []*spectypes.PartialSignatureMessage{{
						PartialSignature: keySet.Shares[op].SignByte(root[:]).Serialize(),
						SigningRoot:      root,
						Signer:           op,
						ValidatorIndex:   share.ValidatorIndex,
					}},
				}
			}

			// Operator 1's share carries operator 2's signature, so it fails verification.
			bad := partial(1)
			bad.Messages[0].PartialSignature = partial(2).Messages[0].PartialSignature
			require.NoError(t, r.ProcessPreConsensus(ctx, logger, bad))
			require.NoError(t, r.ProcessPreConsensus(ctx, logger, partial(2)))
			err = r.ProcessPreConsensus(ctx, logger, partial(3))
			require.ErrorContains(t, err, "invalid signatures")
			require.True(t, isRecoverableReconstructError(err))
			require.Zero(t, submitted())
			require.Empty(t, concluded, "the failed reconstruct is recoverable, so the duty is not concluded failed")

			require.NoError(t, r.ProcessPreConsensus(ctx, logger, partial(4)))
			require.Equal(t, 1, submitted())
			requireConcluded(t, concluded, dutyOutcomeSucceeded)
		})
	}
}

// observeDutyConclusion arms a buffered conclusion channel on b, so a test reads the duty's outcome directly
// rather than through the deadline watcher.
func observeDutyConclusion(b *BaseRunner) chan dutyConclusion {
	concluded := make(chan dutyConclusion, 1)
	b.dutyConcluded = concluded
	return concluded
}

// requireConcluded checks that the duty has concluded with the want outcome.
func requireConcluded(t *testing.T, concluded chan dutyConclusion, want dutyOutcome) {
	t.Helper()
	select {
	case c := <-concluded:
		require.Equal(t, want, c.outcome, "reason: %v", c.reason)
	default:
		t.Fatalf("the duty has not concluded, want %s", want)
	}
}
