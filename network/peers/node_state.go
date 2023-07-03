package peers

// NodeState is the state of the node
type NodeState int32

func (ns NodeState) String() string {
	switch ns {
	case StatePruned:
		return "pruned"
	case StateReady:
		return "ready"
	default:
		return "unknown"
	}
}

var (
	// StatePruned is the state for pruned nodes
	StatePruned NodeState = -1
	// StateUnknown is the state for unknown peers
	StateUnknown NodeState = 0
	// StateReady is the state for a connected, identified node
	StateReady NodeState = 2
)
