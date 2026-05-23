//go:build real_bls
// +build real_bls

// Real-BLS test suite. Gated behind the `real_bls` build tag so default
// `go test` runs stay fast (stub crypto). Run via:
//
//	make consensustest-real-bls
//
// or
//
//	go test -tags=real_bls -v ./protocol/v2/consensustest/...
//
// This suite exercises the OBFT adapter's real-BLS code path (cfg.BLSKeys
// set → blsbackend.New / blsbackend.NewKyberSigner / blsbackend.NewTLockIBE)
// across cluster sizes and scenarios. QBFT is already on real RSA in all
// modes (testingutils.TestKeySet provides per-op RSA keys), so QBFT cells
// here just confirm the cross-protocol matrix holds with the OBFT-side
// crypto upgrade.
//
// First-iteration coverage: single-sim per (n, K, scenario) cell. The plan's
// 40/30/20/10 n-distribution and ~6000-sim deep sweep are aspirational —
// current ~17s wall time leaves room to scale up later by adding seed sweeps,
// network-jitter variation, and additional byz patterns.
package consensustest_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
	qbftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/qbft"
)

// blsKeyCache memoizes per-cluster-size BLS key generation. ct.GenerateBLSKeys
// is ~100-200ms per call (BLS key generation is slow); caching across the
// suite keeps total runtime bounded.
var (
	blsKeyCacheMu sync.Mutex
	blsKeyCache   = make(map[int]*ct.BLSKeys)
)

func cachedBLSKeys(t *testing.T, n int) *ct.BLSKeys {
	t.Helper()
	blsKeyCacheMu.Lock()
	defer blsKeyCacheMu.Unlock()
	if k, ok := blsKeyCache[n]; ok {
		return k
	}
	keys, err := ct.GenerateBLSKeys(ct.MakeOperators(n))
	require.NoErrorf(t, err, "GenerateBLSKeys(n=%d)", n)
	blsKeyCache[n] = keys
	return keys
}

// realBLSConfig builds a SimConfig with real BLS keys for cluster size n at
// the given BTT. Mirrors baseSweepConfig but with cfg.BLSKeys populated.
func realBLSConfig(t *testing.T, n int, btt time.Duration) ct.SimConfig {
	t.Helper()
	cfg := ct.SimConfig{
		N:                    n,
		Operators:            ct.MakeOperators(n),
		SlotDuration:         12 * time.Second,
		RelayCutoff:          4 * time.Second,
		HeaderSubmitHeadroom: 100 * time.Millisecond,
		BTT:                  btt,
		Network:              ct.ConstantDelay{D: btt},
		Host:                 ct.HostAllValid{},
		Byz:                  ct.ByzPattern{Kind: ct.ByzNone},
		Seed:                 1,
		BLSKeys:              cachedBLSKeys(t, n),
	}
	return cfg
}

// TestRealBLS_Healthy_AllClusterSizes — healthy decides under real BLS at
// every supported cluster size. Validates the OBFT adapter's threshold-IBE +
// real-BLS signing path end-to-end at n=4,7,10,13.
func TestRealBLS_Healthy_AllClusterSizes(t *testing.T) {
	for _, n := range ct.ClusterSizes {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			cfg := realBLSConfig(t, n, 200*time.Millisecond)
			out, err := obftadapter.Protocol{}.Run(cfg)
			require.NoError(t, err)
			require.Truef(t, out.Decided, "n=%d real-BLS Healthy must decide", n)
			require.Equalf(t, 0, out.DecidedRound,
				"n=%d real-BLS Healthy must decide at fastest path", n)
			rep := ct.ComputeSafetyReport(out)
			require.Truef(t, rep.SingleV, "n=%d real-BLS SingleV: %s", n, rep)
			require.Truef(t, rep.NoOfflineDoubleV,
				"n=%d real-BLS NoOfflineDoubleV: %s", n, rep)
			t.Logf("n=%d real-BLS: decided at %v on L_%d", n, out.DecisionTime, out.DecidedRound)
		})
	}
}

// TestRealBLS_Catalog_n4 — full catalog at n=4 under real BLS. Per-scenario
// outcomes must match the canonical (stub-mode) matrix — verifies that real
// crypto doesn't change protocol-level outcomes for any scenario.
func TestRealBLS_Catalog_n4(t *testing.T) {
	cfg := realBLSConfig(t, 4, 200*time.Millisecond)
	for _, p := range []ct.Protocol{obftadapter.Protocol{}, qbftadapter.QBFT{}} {
		for _, s := range ct.Catalog {
			r := ct.RunScenarioOnProtocol(t, p, s, cfg)
			require.Truef(t, r.Match, "%s/%s real-BLS mismatch: %s", p.Name(), s.Name, r.Why)
		}
	}
}

// TestRealBLS_Catalog_n7_Diagnostic walks the full catalog at n=7 (f=2)
// with real BLS crypto. Per-scenario Expect values are calibrated for
// n=4 and are *not* asserted here — outcome-class drift at n=7 is
// expected and merely logged. The only hard-failure mode is a safety-
// invariant violation, which RunScenarioOnProtocol panics on
// regardless. Name is suffixed `_Diagnostic` so the lack of cell-level
// assertions is clear at-a-glance; a regression that wants n=7 to be
// strict should add a separate `TestRealBLS_Catalog_n7_Enforced` with
// per-scenario expectations calibrated at the n=7 operating point.
func TestRealBLS_Catalog_n7_Diagnostic(t *testing.T) {
	cfg := realBLSConfig(t, 7, 200*time.Millisecond)
	for _, p := range []ct.Protocol{obftadapter.Protocol{}, qbftadapter.QBFT{}} {
		for _, s := range ct.Catalog {
			r := ct.RunScenarioOnProtocol(t, p, s, cfg)
			if !r.Match && !r.Skipped {
				t.Logf("n=7 %s/%s: %s (canonical n=4 expectation; mismatch logged, not asserted)", p.Name(), s.Name, r.Why)
			}
		}
	}
}

// TestRealBLS_Seeds — Healthy under jittered network, multiple seeds. Each
// seed must produce a clean safety report; outcomes (decided / not) may vary
// under jitter.
func TestRealBLS_Seeds(t *testing.T) {
	const seedCount = 10
	for seed := int64(1); seed <= seedCount; seed++ {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			cfg := realBLSConfig(t, 4, 200*time.Millisecond)
			cfg.Seed = seed
			cfg.Network = ct.LogNormalDelay{Median: 100 * time.Millisecond, Sigma: 0.5}
			out, err := obftadapter.Protocol{}.Run(cfg)
			require.NoError(t, err)
			rep := ct.ComputeSafetyReport(out)
			require.Truef(t, rep.SingleV, "seed=%d SingleV: %s", seed, rep)
			require.Truef(t, rep.NoOfflineDoubleV,
				"seed=%d NoOfflineDoubleV: %s", seed, rep)
		})
	}
}

// TestRealBLS_KSweep_n7 — at n=7 (f=2), K varies from MinK(7)=3 up to N=7.
// Exercises the per-K BLS share count + per-layer IBE encryption depth.
func TestRealBLS_KSweep_n7(t *testing.T) {
	for k := ct.MinK(7); k <= 7; k++ {
		t.Run(fmt.Sprintf("K=%d", k), func(t *testing.T) {
			cfg := realBLSConfig(t, 7, 200*time.Millisecond)
			cfg.K = k
			out, err := obftadapter.Protocol{}.Run(cfg)
			require.NoError(t, err)
			require.Truef(t, out.Decided, "n=7 K=%d real-BLS Healthy must decide", k)
			require.Equalf(t, 0, out.DecidedRound,
				"n=7 K=%d real-BLS Healthy must decide at fastest path", k)
		})
	}
}
