//go:build alan_spec

package networkconfig

import "math"

func init() {
	// Alan spec tests should always run pre-fork.
	TestNetwork.SSV.Forks.Boole = math.MaxUint64
}
