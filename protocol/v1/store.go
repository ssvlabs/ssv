package v1

// MessageStore manages persistence of messages
type MessageStore interface {
	// SaveHighestDecided saves (and potentially overrides) the highest Decided for a specific instance
	SaveHighestDecided(signedMsg ...*SignedMessage) error
	// GetDecided returns decided messages in the given range
	GetDecided(identifier []byte, from uint64, to uint64) ([]SignedMessage, error)
	// SaveDecided returns decided messages in the given range
	SaveDecided(signedMsg ...*SignedMessage) error
}
