package runner

import (
	"context"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

// broadcastPartialSigTypes decodes the captured broadcasts into their partial-sig message types.
func broadcastPartialSigTypes(t *testing.T, msgs []*spectypes.SignedSSVMessage) map[spectypes.PartialSigMsgType]int {
	t.Helper()
	counts := map[spectypes.PartialSigMsgType]int{}
	for _, signed := range msgs {
		psigMsgs := &spectypes.PartialSignatureMessages{}
		require.NoError(t, psigMsgs.Decode(signed.SSVMessage.Data))
		counts[psigMsgs.Type]++
	}
	return counts
}

// End-to-end request-auth convergence riding the §5 duty (issue #2962 B1): executing the duty
// freezes and broadcasts one auth partial per distinct auth root (token-sharing builders share one);
// stashed peer partials replay into the round; quorum reconstructs the SignedBuilderRequestAuth into the
// shared cache; re-emissions never re-broadcast (auth roots are re-emission-invariant); and a root
// outside the frozen set (config divergence) is a hard error. The §5 preference flow must conclude
// exactly as without builders.
func TestProposerPreferencesRunner_requestAuthConvergence(t *testing.T) {
	keySet := spectestingutils.Testing4SharesSet()
	share := spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex)
	cfg := cloneTestNetworkConfig()
	const quorum = 3

	bn := &prefsTestBeacon{BeaconNode: protocoltesting.NewTestingBeaconNodeWrapped(), dependentRoot: phase0.Root{0xaa}}
	network := protocoltesting.NewTestingNetwork(1, keySet.OperatorKeys[1])
	cache := ssv.NewRequestAuthCache(cfg.EstimatedCurrentSlot)

	builders := []gloas.BuilderEntry{
		{URL: "https://builder-a.example.com"},                       // auth data defaults to the URL bytes
		{URL: "https://builder-b.example.com", AuthData: "0x010203"}, // explicit pre-agreed bytes
		{URL: "https://builder-d.example.com", AuthData: "0x010203"}, // distinct builder sharing B's token
	}
	require.NoError(t, gloas.ValidateBuilderConfig(gloas.BuilderConfig{Entries: builders}))

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
		Builders:             gloas.BuilderConfig{Entries: builders},
		RequestAuthCache:     cache,
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

	// peerAuthPartial signs the auth every operator derives from the shared builder config, as peer opID.
	peerAuthPartial := func(t *testing.T, opID spectypes.OperatorID, data []byte) *spectypes.PartialSignatureMessages {
		t.Helper()
		auth := &gloas.BuilderRequestAuth{Data: data, Slot: proposalSlot}
		domain, err := bn.DomainData(context.Background(), cfg.EstimatedEpochAtSlot(proposalSlot), phase0.DomainType(spectypes.DomainBuilderRequestAuth))
		require.NoError(t, err)
		root, err := spectypes.ComputeETHSigningRoot(auth, domain)
		require.NoError(t, err)
		sig := keySet.Shares[opID].SignByte(root[:])
		return &spectypes.PartialSignatureMessages{
			Type: spectypes.RequestAuthPartialSig,
			Slot: proposalSlot,
			Messages: []*spectypes.PartialSignatureMessage{{
				PartialSignature: sig.Serialize(),
				SigningRoot:      root,
				Signer:           opID,
				ValidatorIndex:   share.ValidatorIndex,
			}},
		}
	}
	builderAData := []byte("https://builder-a.example.com")
	builderBData := []byte{0x01, 0x02, 0x03}

	ctx := context.Background()
	logger := zap.NewNop()

	// Builder A's peer partials arrive before our duty (emission skew): retryable, stashed.
	for _, op := range []spectypes.OperatorID{2, 3, 4} {
		err := disp.ProcessPreConsensus(ctx, logger, peerAuthPartial(t, op, builderAData))
		require.Error(t, err)
		require.True(t, IsRetryable(err))
	}

	// Our emission: one preference partial plus one auth partial per distinct auth root goes out —
	// builders B and D share a token, so they share a root and a single broadcast; the stash
	// replays builder A's peer partials to quorum, and its auth lands in the cache.
	require.NoError(t, disp.StartNewDuty(ctx, logger, duty, quorum))
	types := broadcastPartialSigTypes(t, network.BroadcastedMsgs)
	require.Equal(t, 1, types[spectypes.ProposerPreferencesPartialSig])
	require.Equal(t, 2, types[spectypes.RequestAuthPartialSig], "one auth partial per distinct root; the token-sharing pair broadcasts once")

	auths := cache.Get(proposalSlot)
	require.Len(t, auths, 1, "builder A reached quorum via stash replay")
	authA := auths[gloas.BuilderIdentity("https://builder-a.example.com", builderAData)]
	require.NotNil(t, authA)
	require.Equal(t, builderAData, authA.Message.Data)
	require.Equal(t, proposalSlot, authA.Message.Slot)

	// The §5 preference flow concluded normally alongside (stash had no preference partials, so no
	// submit yet — its quorum is driven separately below to prove full independence).
	require.Empty(t, bn.submitted)

	// The shared-token root's peer partials arrive live; ONE reconstruction must serve BOTH builder
	// relationships that agreed on those bytes (the root derives from (data, slot), not the URL).
	for _, op := range []spectypes.OperatorID{2, 3, 4} {
		require.NoError(t, disp.ProcessPreConsensus(ctx, logger, peerAuthPartial(t, op, builderBData)))
	}
	auths = cache.Get(proposalSlot)
	require.Len(t, auths, 3)
	require.NotNil(t, auths[gloas.BuilderIdentity("https://builder-b.example.com", builderBData)])
	require.NotNil(t, auths[gloas.BuilderIdentity("https://builder-d.example.com", builderBData)],
		"a builder sharing another's token must get its own cache entry from the shared reconstruction")

	// Phase 3: each reconstruction also submits the builder preferences to the beacon node. Across the
	// two reconstructions (builder A via stash replay, then the B/D shared root) every configured builder
	// is submitted, each carrying its reconstructed auth.
	submittedURLs := make([]string, 0, 3) // the three configured builders asserted below
	for _, batch := range bn.submittedBuilderPrefs {
		for _, e := range batch {
			submittedURLs = append(submittedURLs, e.URL)
			require.NotNil(t, e.Auth, "each submitted preference carries the reconstructed auth")
		}
	}
	require.ElementsMatch(t, []string{
		"https://builder-a.example.com",
		"https://builder-b.example.com",
		"https://builder-d.example.com",
	}, submittedURLs)

	// A re-emission for the same slot re-freezes but never re-broadcasts an auth (roots are
	// re-emission-invariant and already out) — only the §5 side decides re-broadcast on its own rules.
	broadcastsBefore := len(network.BroadcastedMsgs)
	require.NoError(t, disp.StartNewDuty(ctx, logger, duty, quorum))
	types = broadcastPartialSigTypes(t, network.BroadcastedMsgs[broadcastsBefore:])
	require.Zero(t, types[spectypes.RequestAuthPartialSig], "re-emission must not re-broadcast auth partials")

	// A partial over a root we never froze (divergent peer config) is a hard error, not retryable.
	err = disp.ProcessPreConsensus(ctx, logger, peerAuthPartial(t, 2, []byte("https://rogue.example.com")))
	require.ErrorContains(t, err, "unknown request-auth signing root")
	require.False(t, IsRetryable(err))
}

// A node without builders (never configured, or disabled for a remote signer) never freezes auth
// roots, so peer auth partials for a started duty must fail hard — retrying cannot help. Only a
// partial racing the duty start stays retryable.
func TestProposerPreferencesRunner_requestAuthWithoutBuilders(t *testing.T) {
	keySet := spectestingutils.Testing4SharesSet()
	share := spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex)
	cfg := cloneTestNetworkConfig()

	bn := &prefsTestBeacon{BeaconNode: protocoltesting.NewTestingBeaconNodeWrapped(), dependentRoot: phase0.Root{0xaa}}
	runnerIface, err := NewProposerPreferencesRunner(ProposerPreferencesRunnerOptions{
		BaseRunnerOptions: BaseRunnerOptions{
			NetworkConfig:  cfg,
			Share:          map[phase0.ValidatorIndex]*spectypes.Share{share.ValidatorIndex: share},
			Beacon:         bn,
			Network:        protocoltesting.NewTestingNetwork(1, keySet.OperatorKeys[1]),
			Signer:         ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager()),
			OperatorSigner: spectestingutils.NewOperatorSigner(keySet, 1),
		},
		FeeRecipientProvider: fixedFeeRecipientProvider{addr: bellatrix.ExecutionAddress{0xfe}},
		GasLimit:             36_000_000,
	})
	require.NoError(t, err)
	disp := runnerIface.(*ProposerPreferencesRunner)

	proposalSlot := cfg.EstimatedCurrentSlot() + 5
	require.NoError(t, disp.StartNewDuty(context.Background(), zap.NewNop(), &spectypes.ValidatorDuty{
		Type:           spectypes.BNRoleProposerPreferences,
		PubKey:         spectestingutils.TestingValidatorPubKey,
		Slot:           proposalSlot,
		ValidatorIndex: share.ValidatorIndex,
	}, 3))

	auth := &gloas.BuilderRequestAuth{Data: []byte("https://builder.example.com"), Slot: proposalSlot}
	domain, err := bn.DomainData(context.Background(), cfg.EstimatedEpochAtSlot(proposalSlot), phase0.DomainType(spectypes.DomainBuilderRequestAuth))
	require.NoError(t, err)
	root, err := spectypes.ComputeETHSigningRoot(auth, domain)
	require.NoError(t, err)
	sig := keySet.Shares[2].SignByte(root[:])

	err = disp.ProcessPreConsensus(context.Background(), zap.NewNop(), &spectypes.PartialSignatureMessages{
		Type: spectypes.RequestAuthPartialSig,
		Slot: proposalSlot,
		Messages: []*spectypes.PartialSignatureMessage{{
			PartialSignature: sig.Serialize(),
			SigningRoot:      root,
			Signer:           2,
			ValidatorIndex:   share.ValidatorIndex,
		}},
	})
	require.ErrorContains(t, err, "no builders configured")
	require.False(t, IsRetryable(err), "no root can ever be frozen here, so retrying cannot help")
}

// The request-auth round must not disturb the §5 preference lifecycle: with builders configured,
// the preference still submits on its own quorum, and auth collection keeps running after the §5
// duty has already succeeded (no succeeded-gate on the auth path).
func TestProposerPreferencesRunner_requestAuthAfterPreferenceSuccess(t *testing.T) {
	keySet := spectestingutils.Testing4SharesSet()
	share := spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex)
	cfg := cloneTestNetworkConfig()
	const quorum = 3
	const gasLimit = 36_000_000
	feeRecipient := bellatrix.ExecutionAddress{0xfe}

	bn := &prefsTestBeacon{BeaconNode: protocoltesting.NewTestingBeaconNodeWrapped(), dependentRoot: phase0.Root{0xaa}}
	network := protocoltesting.NewTestingNetwork(1, keySet.OperatorKeys[1])
	cache := ssv.NewRequestAuthCache(cfg.EstimatedCurrentSlot)
	builders := []gloas.BuilderEntry{{URL: "https://builder-a.example.com"}}

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
		Builders:             gloas.BuilderConfig{Entries: builders},
		RequestAuthCache:     cache,
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
	require.NoError(t, disp.StartNewDuty(context.Background(), zap.NewNop(), duty, quorum))

	// Drive the §5 preference to quorum first: the duty succeeds and submits.
	peerPreferencePartial := func(t *testing.T, opID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
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
	ctx := context.Background()
	logger := zap.NewNop()
	for _, op := range []spectypes.OperatorID{2, 3, 4} {
		require.NoError(t, disp.ProcessPreConsensus(ctx, logger, peerPreferencePartial(t, op)))
	}
	require.Len(t, bn.submitted, 1, "§5 preference must submit on its quorum")

	// Auth partials arriving after the §5 success must still be collected and reconstructed.
	authData := []byte("https://builder-a.example.com")
	auth := &gloas.BuilderRequestAuth{Data: authData, Slot: proposalSlot}
	domain, err := bn.DomainData(ctx, cfg.EstimatedEpochAtSlot(proposalSlot), phase0.DomainType(spectypes.DomainBuilderRequestAuth))
	require.NoError(t, err)
	root, err := spectypes.ComputeETHSigningRoot(auth, domain)
	require.NoError(t, err)
	for _, op := range []spectypes.OperatorID{2, 3, 4} {
		sig := keySet.Shares[op].SignByte(root[:])
		require.NoError(t, disp.ProcessPreConsensus(ctx, logger, &spectypes.PartialSignatureMessages{
			Type: spectypes.RequestAuthPartialSig,
			Slot: proposalSlot,
			Messages: []*spectypes.PartialSignatureMessage{{
				PartialSignature: sig.Serialize(),
				SigningRoot:      root,
				Signer:           op,
				ValidatorIndex:   share.ValidatorIndex,
			}},
		}))
	}
	require.Len(t, cache.Get(proposalSlot), 1,
		"auth must reconstruct even after the §5 preference concluded the duty")
}

// A re-emission that concludes immediately (unchanged preference → not-required, e.g. an
// indices-change re-emit) replaces the sub-runner and its containers. The stash replay into the
// replacement must run even though the duty is already concluded — peers broadcast their partials
// exactly once, so an auth still short of quorum could otherwise never reach it.
func TestProposerPreferencesRunner_requestAuthSurvivesConcludedReemission(t *testing.T) {
	keySet := spectestingutils.Testing4SharesSet()
	share := spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex)
	cfg := cloneTestNetworkConfig()
	const quorum = 3
	const gasLimit = 36_000_000
	feeRecipient := bellatrix.ExecutionAddress{0xfe}

	bn := &prefsTestBeacon{BeaconNode: protocoltesting.NewTestingBeaconNodeWrapped(), dependentRoot: phase0.Root{0xaa}}
	network := protocoltesting.NewTestingNetwork(1, keySet.OperatorKeys[1])
	cache := ssv.NewRequestAuthCache(cfg.EstimatedCurrentSlot)
	builders := []gloas.BuilderEntry{{URL: "https://builder-a.example.com"}}

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
		Builders:             gloas.BuilderConfig{Entries: builders},
		RequestAuthCache:     cache,
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
	ctx := context.Background()
	logger := zap.NewNop()

	peerPreferencePartial := func(t *testing.T, opID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
		t.Helper()
		prefs := &gloas.ProposerPreferences{
			DependentRoot:  bn.dependentRoot,
			ProposalSlot:   proposalSlot,
			ValidatorIndex: share.ValidatorIndex,
			FeeRecipient:   feeRecipient,
			TargetGasLimit: gasLimit,
		}
		domain, err := bn.DomainData(ctx, cfg.EstimatedEpochAtSlot(proposalSlot), phase0.DomainType(spectypes.DomainProposerPreferences))
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
	peerAuthPartial := func(t *testing.T, opID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
		t.Helper()
		auth := &gloas.BuilderRequestAuth{Data: []byte("https://builder-a.example.com"), Slot: proposalSlot}
		domain, err := bn.DomainData(ctx, cfg.EstimatedEpochAtSlot(proposalSlot), phase0.DomainType(spectypes.DomainBuilderRequestAuth))
		require.NoError(t, err)
		root, err := spectypes.ComputeETHSigningRoot(auth, domain)
		require.NoError(t, err)
		sig := keySet.Shares[opID].SignByte(root[:])
		return &spectypes.PartialSignatureMessages{
			Type: spectypes.RequestAuthPartialSig,
			Slot: proposalSlot,
			Messages: []*spectypes.PartialSignatureMessage{{
				PartialSignature: sig.Serialize(),
				SigningRoot:      root,
				Signer:           opID,
				ValidatorIndex:   share.ValidatorIndex,
			}},
		}
	}

	require.NoError(t, disp.StartNewDuty(ctx, logger, duty, quorum))

	// The §5 preference reaches quorum and submits: the duty concludes succeeded.
	for _, op := range []spectypes.OperatorID{2, 3, 4} {
		require.NoError(t, disp.ProcessPreConsensus(ctx, logger, peerPreferencePartial(t, op)))
	}
	require.Len(t, bn.submitted, 1)

	// Two auth partials arrive — one short of quorum.
	for _, op := range []spectypes.OperatorID{2, 3} {
		require.NoError(t, disp.ProcessPreConsensus(ctx, logger, peerAuthPartial(t, op)))
	}
	require.Empty(t, cache.Get(proposalSlot))

	// An unchanged re-emission concludes not-required immediately; the stash must still replay
	// into the replacement's fresh container.
	require.NoError(t, disp.StartNewDuty(ctx, logger, duty, quorum))

	// The third partial completes the quorum.
	require.NoError(t, disp.ProcessPreConsensus(ctx, logger, peerAuthPartial(t, 4)))
	require.Len(t, cache.Get(proposalSlot), 1,
		"stash replay into the concluded replacement must let the auth quorum complete")
}
