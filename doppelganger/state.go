package doppelganger

// doppelgangerState tracks the validator's state in Doppelganger Protection.
type doppelgangerState struct {
	remainingEpochs uint64 // The number of epochs that must be checked before it's considered safe.
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

// Status represents the state of a validator.
type Status int

const (
	SigningEnabled        Status = iota // Validator can sign.
	SigningDisabled                     // Validator is waiting for epochs to pass.
	UnknownToDoppelganger               // Validator state is unknown (error case).
)
