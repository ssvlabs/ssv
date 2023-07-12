package eventdatahandler

import (
	"github.com/ethereum/go-ethereum/common"

	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

type TaskExecutor interface {
	AddValidator(publicKey []byte) error
	RemoveValidator(publicKey []byte) error
	LiquidateCluster(owner common.Address, operatorIDs []uint64, toLiquidate []*ssvtypes.SSVShare) error
	ReactivateCluster(owner common.Address, operatorIDs []uint64, toEnable []*ssvtypes.SSVShare) error
	UpdateFeeRecipient(owner, recipient common.Address) error
}
