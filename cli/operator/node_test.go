package operator

import (
	"context"
	"fmt"
	"testing"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/doppelganger"
	"github.com/ssvlabs/ssv/eth/executionclient"
	exporterconfig "github.com/ssvlabs/ssv/exporter/config"
	"github.com/ssvlabs/ssv/hprobe"
	ibftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/network"
	networkcommons "github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/networkconfig"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// shareWithCommittee builds a minimal SSVShare whose committee is the given operator IDs — enough
// to exercise BooleCommitteeSubnet/AlanCommitteeSubnet, which derive solely from the committee.
func shareWithCommittee(operatorIDs ...spectypes.OperatorID) *ssvtypes.SSVShare {
	committee := make([]*spectypes.ShareMember, len(operatorIDs))
	for i, id := range operatorIDs {
		committee[i] = &spectypes.ShareMember{Signer: id}
	}
	return &ssvtypes.SSVShare{
		Share: spectypes.Share{Committee: committee},
	}
}

// Test_operatorActiveSubnets locks in the fork-gated subnet-counting behavior extracted from
// applyDynamicMaxPeers: post-fork only Boole subnets count, pre-fork both Alan and Boole subnets
// count in independent bitmaps, and validators landing on an already-seen subnet don't inflate the
// count. It also pins the two cases iurii-ssv flagged: a Boole/Alan subnet-number collision must not
// under-count, and an empty committee (BooleCommitteeSubnet -> UnknownSubnetId) must not inflate.
func TestOperatorActiveSubnets(t *testing.T) {
	shareA := shareWithCommittee(1, 2, 3, 4)
	shareB := shareWithCommittee(5, 6, 7, 8)
	shareADup := shareWithCommittee(1, 2, 3, 4) // same committee as shareA => same subnets

	t.Run("post-fork counts only distinct Boole subnets", func(t *testing.T) {
		validators := []*ssvtypes.SSVShare{shareA, shareB}

		require.Equal(t, 2, operatorActiveSubnets(validators, true))
	})

	t.Run("pre-fork counts both Alan and Boole subnets", func(t *testing.T) {
		validators := []*ssvtypes.SSVShare{shareA, shareB}

		require.Equal(t, 4, operatorActiveSubnets(validators, false))
	})

	t.Run("validators sharing a committee dedupe to a single subnet", func(t *testing.T) {
		validators := []*ssvtypes.SSVShare{shareA, shareADup}

		require.Equal(t, 1, operatorActiveSubnets(validators, true))
	})

	t.Run("pre-fork counts Boole and Alan sets independently on subnet collision", func(t *testing.T) {
		// A validator whose Alan subnet coincides with its own Boole subnet must still be counted
		// once per set (2), not deduped to 1 by a shared bitmap.
		v := findSubnetCollision(t)
		require.Equal(t, v.BooleCommitteeSubnet(), v.AlanCommitteeSubnet())

		require.Equal(t, 2, operatorActiveSubnets([]*ssvtypes.SSVShare{v}, false))
	})

	t.Run("empty committee does not inflate the count", func(t *testing.T) {
		empty := shareWithCommittee() // BooleCommitteeSubnet => UnknownSubnetId
		require.Equal(t, uint64(networkcommons.UnknownSubnetId), empty.BooleCommitteeSubnet())

		validators := []*ssvtypes.SSVShare{empty, empty, shareA}
		require.Equal(t, 1, operatorActiveSubnets(validators, true))
	})

	t.Run("no validators yields no active subnets", func(t *testing.T) {
		require.Equal(t, 0, operatorActiveSubnets(nil, true))
	})
}

// findSubnetCollision returns a share whose Boole and Alan committee subnets land on the same subnet
// number, so a shared bitmap would wrongly dedupe them. Searches committees deterministically and
// skips the test if none is found within the bound (both are mod-128 hashes, so a hit is expected).
func findSubnetCollision(t *testing.T) *ssvtypes.SSVShare {
	t.Helper()
	for id := spectypes.OperatorID(1); id <= 4096; id++ {
		v := shareWithCommittee(id, id+1, id+2, id+3)
		if v.BooleCommitteeSubnet() == v.AlanCommitteeSubnet() {
			return v
		}
	}
	t.Skip("no Boole/Alan subnet collision found within search bound")
	return nil
}

// Test_buildNode_invalidConfigReturns verifies buildNode() returns the configuration error instead of
// calling logger.Fatal (which would os.Exit the test process) when resolveAndValidate fails.
// A conflicting signing config trips resolveAndValidate — the first thing buildNode() does — before
// any network I/O, so the call is safe to make in-process.
func Test_buildNode_invalidConfigReturns(t *testing.T) {
	c := config{}
	c.SSVSigner.Endpoint = testSignerEndpoint
	c.OperatorPrivateKey = testOperatorKey // remote + local signing => rejected by resolveAndValidate

	_, err := buildNode(context.Background(), &c, zap.NewNop())
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot enable both remote signing")
}

// stubBeaconClient satisfies the in-package beaconClient interface by embedding it (nil): it
// promotes every method so the type compiles as a beaconClient, while implementing only what the
// test path actually invokes. newNode() starts no goroutines and — with Doppelganger disabled —
// invokes only one beacon method synchronously during assembly (SetProposalPreparationsProvider),
// so that's the sole override needed.
type stubBeaconClient struct {
	beaconClient
}

// SetProposalPreparationsProvider is the one beacon method invoked synchronously during assembly
// (operator.New wires it into the fee-recipient controller); a no-op satisfies the smoke test.
func (stubBeaconClient) SetProposalPreparationsProvider(func() ([]*eth2apiv1.ProposalPreparation, error)) {
}

// stubExecutionClient satisfies executionclient.Provider the same way. newNode() only stores the
// EL client (it is consumed in start(), not here), so no method is invoked during assembly.
type stubExecutionClient struct {
	executionclient.Provider
}

// Close is a no-op: node.close() (invoked by the smoke tests during teardown) calls it, so it must
// not fall through to the nil embedded Provider.
func (stubExecutionClient) Close() error { return nil }

// Test_newNode_wiresOperatorNode is the in-process smoke test for the newNode() seam: with
// stubbed beacon/EL clients and a minimal operator-mode config, the full wiring graph
// (db → storage → keys → p2p → validator controller → operator node) must construct without
// error. It exercises everything up to — but not including — start()'s network bring-up, which
// performs real socket I/O. The doppelganger dimension covers buildDoppelganger's real-handler
// path (enabled) alongside the no-op path (disabled).
func Test_newNode_wiresOperatorNode(t *testing.T) {
	for _, tc := range []struct {
		name           string
		doppelgangerOn bool
	}{
		{name: "doppelganger disabled", doppelgangerOn: false},
		{name: "doppelganger enabled", doppelgangerOn: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			logger := zap.NewNop()

			operatorPrivKey, err := keys.GeneratePrivateKey()
			require.NoError(t, err)

			netCfg := *networkconfig.TestNetwork
			networkConfig := &netCfg

			cfg := &config{}
			cfg.OperatorPrivateKey = operatorPrivKey.Base64()
			cfg.DBOptions.Path = t.TempDir()
			// Keep all long-lived servers off; they only matter in start().
			cfg.MetricsAPIPort = 0
			cfg.SSVAPIPort = 0
			cfg.WsAPIPort = 0
			cfg.EnableDoppelgangerProtection = tc.doppelgangerOn

			res := resolved{mode: modeOperator, usingPrivKey: true}

			a, err := newNode(ctx, cfg, logger, res, networkConfig, stubBeaconClient{}, stubExecutionClient{})
			require.NoError(t, err)
			require.NotNil(t, a)
			require.NotNil(t, a.db)
			require.NotNil(t, a.nodeStorage)
			require.NotNil(t, a.p2pNetwork)
			require.NotNil(t, a.validatorCtrl)
			require.NotNil(t, a.operatorNode)

			// buildDoppelganger returns the no-op handler when protection is disabled and a real
			// handler when enabled.
			_, isNoOp := a.doppelgangerHandler.(doppelganger.NoOpHandler)
			require.Equal(t, !tc.doppelgangerOn, isNoOp, "doppelganger handler must match the configured protection")

			// Mirror production teardown ordering: cancel the ctx, then close. newNode starts no
			// goroutines, so nothing is racing here — the p2p network was constructed but never
			// Setup/Start'd.
			cancel()
			require.NoError(t, a.close())
		})
	}
}

// Test_newNode_wiresExporterNode mirrors the operator smoke test for the exporter paths: with no
// signing identity, it asserts newNode() wires the graph for both exporter modes and that the
// mode-specific divergences hold — no key manager in either, and a duty-trace collector only in
// archive mode.
func Test_newNode_wiresExporterNode(t *testing.T) {
	for _, tc := range []struct {
		name          string
		mode          nodeMode
		exporterMode  string
		wantCollector bool
	}{
		{name: "standard", mode: modeExporterStandard, exporterMode: exporterconfig.ModeStandard, wantCollector: false},
		{name: "archive", mode: modeExporterArchive, exporterMode: exporterconfig.ModeArchive, wantCollector: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			logger := zap.NewNop()

			netCfg := *networkconfig.TestNetwork
			networkConfig := &netCfg

			cfg := &config{}
			cfg.DBOptions.Path = t.TempDir()
			cfg.ExporterOptions.Enabled = true
			cfg.ExporterOptions.Mode = tc.exporterMode
			cfg.MetricsAPIPort = 0
			cfg.SSVAPIPort = 0
			cfg.WsAPIPort = 0

			// Exporter mode resolves no signing flags; mode alone drives the divergence.
			res := resolved{mode: tc.mode}

			a, err := newNode(ctx, cfg, logger, res, networkConfig, stubBeaconClient{}, stubExecutionClient{})
			require.NoError(t, err)
			require.NotNil(t, a)
			require.NotNil(t, a.db)
			require.NotNil(t, a.nodeStorage)
			require.NotNil(t, a.p2pNetwork)
			require.NotNil(t, a.validatorCtrl)
			require.NotNil(t, a.operatorNode)
			require.Nil(t, a.keyManager, "exporter nodes have no key manager")

			if tc.wantCollector {
				require.NotNil(t, a.collector, "archive mode wires a duty-trace collector")
			} else {
				require.Nil(t, a.collector, "standard mode has no duty-trace collector")
			}

			// Mirror production teardown ordering: cancel the ctx, then close (newNode starts no goroutines).
			cancel()
			require.NoError(t, a.close())
		})
	}
}

// Test_newNode_closesDBOnAssemblyFailure locks in newNode()'s error-only cleanup contract: when
// assembly fails after the db is opened, the named-return-err defer must Close the db. It forces a
// failure in setupP2P (an invalid NetworkPrivateKey) — which runs after openNodeDB — and proves the
// db was closed by reopening the badger dir: a leaked handle would still hold the directory lock and
// fail the reopen. Guards against a future `:=` shadow of the named err silently breaking cleanup.
func Test_newNode_closesDBOnAssemblyFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := zap.NewNop()

	operatorPrivKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	netCfg := *networkconfig.TestNetwork
	networkConfig := &netCfg

	cfg := &config{}
	cfg.OperatorPrivateKey = operatorPrivKey.Base64()
	cfg.DBOptions.Path = t.TempDir()
	cfg.MetricsAPIPort = 0
	cfg.SSVAPIPort = 0
	cfg.WsAPIPort = 0
	// An invalid network key makes setupP2P fail — but only after openNodeDB has run, so the
	// failure exercises the db-close defer rather than failing before the db is even opened.
	cfg.NetworkPrivateKey = "not-a-valid-network-key"

	res := resolved{mode: modeOperator, usingPrivKey: true}

	a, err := newNode(ctx, cfg, logger, res, networkConfig, stubBeaconClient{}, stubExecutionClient{})
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to setup network private key") // i.e. failed in setupP2P, after db-open
	require.Nil(t, a)

	// newNode starts no goroutines, so the failed assembly leaves nothing running.
	//
	// The error-only defer must have closed the db: a fresh badger open of the same dir succeeds
	// only if the previous handle released its directory lock.
	reopened, err := kv.New(zap.NewNop(), basedb.Options{Path: cfg.DBOptions.Path, Ctx: context.Background()})
	require.NoError(t, err, "newNode must Close the db on assembly failure; a leaked handle keeps the badger dir locked")
	require.NoError(t, reopened.Close())
}

// recordingP2PNetwork is a stub network.P2PNetwork that records Setup/Start instead of performing
// real socket I/O, and satisfies p2pv1.HealthChecker (via Healthy) so startNetwork's health-prober
// registration — a.p2pNetwork.(p2pv1.HealthChecker) — succeeds.
type recordingP2PNetwork struct {
	network.P2PNetwork
	setupCalled bool
	startCalled bool
}

func (r *recordingP2PNetwork) Setup() error                  { r.setupCalled = true; return nil }
func (r *recordingP2PNetwork) Start() error                  { r.startCalled = true; return nil }
func (r *recordingP2PNetwork) Healthy(context.Context) error { return nil }

// Test_node_startNetwork_wiresStatsRegardlessOfDynamicMaxPeers is the CLI-level regression for
// the DynamicMaxPeers bug: GetValidatorStats must be wired and the p2p network Set up + Started
// whether or not DynamicMaxPeers is enabled. Re-nesting these under the DynamicMaxPeers flag (the
// original bug) would fail this test.
func Test_node_startNetwork_wiresStatsRegardlessOfDynamicMaxPeers(t *testing.T) {
	for _, dynamicMaxPeers := range []bool{false, true} {
		t.Run(fmt.Sprintf("DynamicMaxPeers=%t", dynamicMaxPeers), func(t *testing.T) {
			stubNet := &recordingP2PNetwork{}
			cfg := &config{}
			cfg.P2pNetworkConfig.DynamicMaxPeers = dynamicMaxPeers

			a := &node{
				logger:     zap.NewNop(),
				cfg:        cfg,
				p2pNetwork: stubNet,
				// validatorCtrl stays nil: startNetwork only *assigns* the GetValidatorStats closure
				// (pubsub invokes it later, not here), so a nil controller is never dereferenced.
			}

			require.NoError(t, a.startNetwork(hprobe.NewHealthProber(zap.NewNop())))

			require.NotNil(t, cfg.P2pNetworkConfig.GetValidatorStats,
				"GetValidatorStats must be wired regardless of DynamicMaxPeers")
			require.True(t, stubNet.setupCalled, "p2p Setup must run regardless of DynamicMaxPeers")
			require.True(t, stubNet.startCalled, "p2p Start must run regardless of DynamicMaxPeers")
		})
	}
}

// Test_startSlotPruning_spawnsContinuousPrunerPerStore checks the wiring the goroutine-free refactor
// introduced: the per-store background pruning is launched through spawn (one per store) rather than
// a bare go, and the synchronous initial GC runs without hanging. The spawn here only counts — it
// doesn't run the pruners — so this asserts the wiring, not the pruning semantics (those live in
// ibft/storage).
func Test_startSlotPruning_spawnsContinuousPrunerPerStore(t *testing.T) {
	db, err := kv.New(zap.NewNop(), basedb.Options{Path: t.TempDir(), Ctx: context.Background()})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	stores := ibftstorage.NewStores()
	stores.Add(spectypes.BNRoleAttester, ibftstorage.New(zap.NewNop(), db, spectypes.BNRoleAttester))
	stores.Add(spectypes.BNRoleProposer, ibftstorage.New(zap.NewNop(), db, spectypes.BNRoleProposer))

	spawned := 0
	spawn := func(func() error) { spawned++ } // count, don't run — testing the wiring, not pruning

	// slot > retain so the initial-GC threshold doesn't underflow; the ticker provider is never
	// invoked because spawn doesn't run the pruners.
	startSlotPruning(context.Background(), spawn, stores, nil, 1000, 100)

	require.Equal(t, 2, spawned, "one continuous-pruner must be spawned per store")
}
