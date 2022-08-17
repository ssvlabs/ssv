package valcheck

// ValueCheck is an interface which validates the proposal value passed to the node.
// It's kept minimal to allow the implementation to have all the check logic.
type ValueCheck interface {
	Check(value []byte) error
}
