package types

// Implementor is an interface that bundles together iBFT implementation specific functions which Instance calls.
// All implementation specific details should be encapsulated on the implementation level.
// For example:
// 		- input value + verification
//		- lambda value + verification
type Implementor interface {
	ValidateValue(value []byte) error
}
