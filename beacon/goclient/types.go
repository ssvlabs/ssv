package goclient

import (
	"math"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

var (
	// FarFutureEpoch is the null representation of an epoch.
	FarFutureEpoch phase0.Epoch = math.MaxUint64
)
