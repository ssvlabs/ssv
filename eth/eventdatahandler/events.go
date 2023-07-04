package eventdatahandler

import (
	"fmt"
)

type EventType int

// Event names
const (
	OperatorAdded = iota
	OperatorRemoved
	ValidatorAdded
	ValidatorRemoved
	ClusterLiquidated
	ClusterReactivated
	FeeRecipientAddressUpdated
)

func (ev EventType) String() string {
	switch ev {
	case OperatorAdded:
		return "OperatorAdded"
	case OperatorRemoved:
		return "OperatorRemoved"
	case ValidatorAdded:
		return "ValidatorAdded"
	case ValidatorRemoved:
		return "ValidatorRemoved"
	case ClusterLiquidated:
		return "ClusterLiquidated"
	case ClusterReactivated:
		return "ClusterReactivated"
	case FeeRecipientAddressUpdated:
		return "FeeRecipientAddressUpdated"
	default:
		return fmt.Sprintf("%d", int(ev))
	}
}
