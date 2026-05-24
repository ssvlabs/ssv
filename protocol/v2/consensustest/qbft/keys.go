package qbft

import (
	"fmt"
	"sync"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
)

// keysetCache / committeeCache memoize the per-N spec test fixtures. Despite
// the name, spectestingutils.TestingNSharesSet() is NOT a package-level
// singleton — each call rebuilds the whole TestKeySet from scratch, ASN.1-
// parsing N operator RSA private keys (plus BLS + ECDSA material). In the
// stress driver newSim runs once per simulation (millions of times), so the
// uncached path spent ~11% of total CPU re-parsing identical RSA keys.
//
// The fixtures are deterministic per N and read-only after construction
// (signing uses the private key without mutating it; verification reads only
// public material), and the spec intends them to be shared, so caching one
// instance per N and handing the same pointer to every sim is safe under the
// framework's per-sim goroutine parallelism.
var (
	keysetCache    sync.Map // int -> *spectestingutils.TestKeySet
	committeeCache sync.Map // int -> *spectypes.CommitteeMember
)

// keysetForN returns the spec testingutils key set for cluster size n.
// Provides BLS shares + RSA operator keys for the cluster, plus the
// committee structure required by qbft.Instance / spectypes.Verify.
//
// Supported sizes: n ∈ {4, 7, 10, 13} per spectestingutils' tabulated sets.
// Cached per N (see keysetCache) so the expensive RSA-key construction runs
// once per cluster size per process rather than once per simulation.
func keysetForN(n int) (*spectestingutils.TestKeySet, error) {
	if v, ok := keysetCache.Load(n); ok {
		return v.(*spectestingutils.TestKeySet), nil
	}
	var ks *spectestingutils.TestKeySet
	switch n {
	case 4:
		ks = spectestingutils.Testing4SharesSet()
	case 7:
		ks = spectestingutils.Testing7SharesSet()
	case 10:
		ks = spectestingutils.Testing10SharesSet()
	case 13:
		ks = spectestingutils.Testing13SharesSet()
	default:
		return nil, fmt.Errorf("qbft adapter: unsupported cluster size n=%d (only 4, 7, 10, 13)", n)
	}
	// LoadOrStore so a race between two first-callers keeps a single shared
	// instance (a redundant build is harmless — the fixtures are identical).
	actual, _ := keysetCache.LoadOrStore(n, ks)
	return actual.(*spectestingutils.TestKeySet), nil
}

// committeeForN returns the (cached) spec CommitteeMember template for cluster
// size n. The returned pointer is shared across sims: callers must treat it as
// read-only and shallow-copy it before mutating per-operator fields (see
// buildInstance, which sets OperatorID on a copy).
func committeeForN(n int) (*spectypes.CommitteeMember, error) {
	if v, ok := committeeCache.Load(n); ok {
		return v.(*spectypes.CommitteeMember), nil
	}
	keys, err := keysetForN(n)
	if err != nil {
		return nil, err
	}
	cm := spectestingutils.TestingCommitteeMember(keys)
	actual, _ := committeeCache.LoadOrStore(n, cm)
	return actual.(*spectypes.CommitteeMember), nil
}
