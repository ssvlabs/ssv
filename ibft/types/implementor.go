package types

// Implementor is an interface that bundles together iBFT implementation specific functions which iBFTInstance calls
type Implementor interface {
	IsLeader(state *State) bool
	NewPrePrepareMsg(state *State) *Message
}
