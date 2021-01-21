package types

import "bytes"

// Compare returns true if both messages are equal.
// DOES NOT compare signatures
func (msg Message) Compare(other Message) bool {
	if msg.Type != other.Type ||
		msg.Round != other.Round ||
		msg.IbftId != msg.IbftId ||
		!bytes.Equal(msg.Lambda, other.Lambda) ||
		!bytes.Equal(msg.InputValue, other.InputValue) {
		return false
	}

	return true
}
