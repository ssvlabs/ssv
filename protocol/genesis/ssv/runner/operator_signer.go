package genesisrunner

import (
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
)

// TODO: implement
type OperatorSigner interface {
	SignSSVMessage(data []byte) ([256]byte, error)
	GetOperatorID() genesisspectypes.OperatorID
}
