package doppelganger

// DoppelgangerState tracks the validator's state in Doppelganger Protection.
type DoppelgangerState struct {
	RemainingEpochs uint64 // The number of epochs that must be checked before it's considered safe.
	IsLive          bool   // The validator's liveness status.
}

// requiresFurtherChecks returns true if the validator is *not* safe to sign yet.
func (ds *DoppelgangerState) requiresFurtherChecks() bool {
	return ds.RemainingEpochs > 0
}

// decreaseRemainingEpochs decreases remaining epochs.
func (ds *DoppelgangerState) decreaseRemainingEpochs() {
	if ds.RemainingEpochs > 0 {
		ds.RemainingEpochs--
	}
}

// DoppelgangerStatus represents the state of a validator.
type DoppelgangerStatus int

const (
	SigningEnabled        DoppelgangerStatus = iota // Validator can sign.
	SigningDisabled                                 // Validator is waiting for epochs to pass.
	UnknownToDoppelganger                           // Validator state is unknown (error case).
)
