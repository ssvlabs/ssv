package types

// Implementor is an interface that bundles together iBFT implementation specific functions which iBFTInstance calls.
// All implementation specific details should be encapsulated on the implementation level.
// For example:
// 		- input value + verification
//		- lambda value + verification
type Implementor interface {
	// IsLeader returns true if iBFTInstance should behave as a leader. Specific leader selection logic depends on the implementation
	IsLeader(state *State) bool

	NewPrePrepareMsg(state *State) *Message
	ValidatePrePrepareMsg(state *State, msg *Message) error
	NewPrepareMsg(state *State) *Message
	ValidatePrepareMsg(state *State, msg *Message) error
	NewCommitMsg(state *State) *Message
	ValidateCommitMsg(state *State, msg *Message) error
}
