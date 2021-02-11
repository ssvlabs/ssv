package validator

// Validator represents the behavior of the data validation between SSV nodes.
// This is needed to make a consensus between SSV nodes, meaning SSV nodes must have the same data
// to make a consensus.
// For instance, attestation data should be the same between nodes during validation process.
type Validator interface {
	Validate(value []byte) error
}
