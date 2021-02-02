package valueImpl

// ValueImplementation is an interface for specific value implementation
type ValueImplementation interface {
	ValidateValue(value []byte) error
	SignValue(value []byte, skByts []byte) ([]byte, error)
}
