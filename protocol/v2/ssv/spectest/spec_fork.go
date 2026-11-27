//go:build !alan_spec

package spectest

import "github.com/attestantio/go-eth2-client/spec/phase0"

// specGloasForkEpoch is the epoch from which the spec's vectors are Gloas-era. The spec is Gloas-only, so all its
// vectors are, whatever their slot.
const specGloasForkEpoch = phase0.Epoch(0)
