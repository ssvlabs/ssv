package val

// ValueValidator is an interface for specific val implementation
type ValueValidator interface {
	Validate(value []byte) error
}
