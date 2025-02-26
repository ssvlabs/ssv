package doppelganger

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/beacon/goclient"
)

// doppelgangerState tracks the validator's state in Doppelganger Protection.
type doppelgangerState struct {
	remainingEpochs phase0.Epoch // The number of epochs that must be checked before it's considered safe.
}

// requiresFurtherChecks returns true if the validator is *not* safe to sign yet.
func (ds *doppelgangerState) requiresFurtherChecks() bool {
	return ds.remainingEpochs > 0
}

// decreaseRemainingEpochs decreases remaining epochs.
func (ds *doppelgangerState) decreaseRemainingEpochs() {
	if ds.remainingEpochs > 0 {
		ds.remainingEpochs--
	}
}

// detectedAsLive returns true if the validator was previously marked as live on another node via liveness checks.
// This means the validator should not be trusted for signing, as it indicates potential duplication.
func (ds *doppelgangerState) detectedAsLive() bool {
	return ds.remainingEpochs == goclient.FarFutureEpoch
}

// markAsLive marks the validator as detected live on another node via liveness checks.
// This means the validator should not be trusted for signing, as it indicates potential duplication.
// The remaining epochs are set to FarFutureEpoch to ensure it is not considered safe until explicitly reset.
func (ds *doppelgangerState) markAsLive() {
	ds.remainingEpochs = goclient.FarFutureEpoch
}
