package leader

import "github.com/bloxapp/ssv/protocol/v1/message"

// Selector is interface to implement the leader selection logic
type Selector interface {
	// Calculate returns the current leader as calculated by the implementation.
	Calculate(round message.Round) uint64
}
