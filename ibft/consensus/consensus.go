package consensus

// Consensus is an interface for specific consensus implementation
type Consensus interface {
	ValidateValue(value []byte) error
}
