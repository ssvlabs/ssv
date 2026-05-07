package qbft

import (
	"fmt"

	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
)

// keysetForN returns the spec testingutils key set for cluster size n.
// Provides BLS shares + RSA operator keys for the cluster, plus the
// committee structure required by qbft.Instance / spectypes.Verify.
//
// Supported sizes: n ∈ {4, 7, 10, 13} per spectestingutils' tabulated sets.
// Generation cost is paid once per process (sets are package-level singletons
// in spectestingutils).
func keysetForN(n int) (*spectestingutils.TestKeySet, error) {
	switch n {
	case 4:
		return spectestingutils.Testing4SharesSet(), nil
	case 7:
		return spectestingutils.Testing7SharesSet(), nil
	case 10:
		return spectestingutils.Testing10SharesSet(), nil
	case 13:
		return spectestingutils.Testing13SharesSet(), nil
	default:
		return nil, fmt.Errorf("qbft adapter: unsupported cluster size n=%d (only 4, 7, 10, 13)", n)
	}
}
