//go:build alan_spec

package spectest

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
)

// specGloasForkEpoch is the epoch from which the spec's vectors are Gloas-era. The Alan vectors predate Gloas, so
// none of their slots reach the spec's testing Gloas epoch.
const specGloasForkEpoch = phase0.Epoch(spectestingutils.ForkEpochGloas)
