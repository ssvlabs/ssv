package types

import (
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// OperatorSigner used for to sign protocol messages
type OperatorSigner interface {
	SignSSVMessage(ssvMsg *spectypes.SSVMessage) ([]byte, error)
	GetOperatorID() spectypes.OperatorID
}
