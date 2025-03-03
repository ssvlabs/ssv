package doppelganger

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// doppelgangerState tracks the validator's state in Doppelganger Protection.
type doppelgangerState struct {
	remainingEpochs phase0.Epoch // The number of epochs that must be not live before it's considered safe.
	observedQuorum  bool         // Whether the validator has observed a quorum of SSV operators.
}

// safe returns true if the validator is safe to sign.
func (ds *doppelgangerState) safe() bool {
	return ds.remainingEpochs == 0 || ds.observedQuorum
}

// decreaseRemainingEpochs decreases remaining epochs.
func (ds *doppelgangerState) decreaseRemainingEpochs() error {
	if ds.remainingEpochs == 0 {
		return fmt.Errorf("attempted to decrease remaining epochs at 0")
	}
	ds.remainingEpochs--
	return nil
}

// markAsLive marks the validator as detected live on another node via liveness checks.
// This means the validator should not be trusted for signing, as it indicates potential duplication.
// The remaining epochs are set to FarFutureEpoch to ensure it is not considered safe until explicitly reset.
func (ds *doppelgangerState) resetRemainingEpochs() {
	ds.remainingEpochs = initialRemainingDetectionEpochs
}
