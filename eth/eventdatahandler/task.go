package eventdatahandler

import (
	"github.com/ethereum/go-ethereum/common"

	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

type Task interface {
	Execute() error
}

type AddValidatorTask struct {
	executor  TaskExecutor
	publicKey []byte
}

func NewAddValidatorTask(executor TaskExecutor, publicKey []byte) *AddValidatorTask {
	return &AddValidatorTask{
		executor:  executor,
		publicKey: publicKey,
	}
}

func (t AddValidatorTask) Execute() error {
	return t.executor.AddValidator(t.publicKey)
}

type RemoveValidatorTask struct {
	executor  TaskExecutor
	publicKey []byte
}

func NewRemoveValidatorTask(executor TaskExecutor, publicKey []byte) *RemoveValidatorTask {
	return &RemoveValidatorTask{
		executor:  executor,
		publicKey: publicKey,
	}
}

func (t RemoveValidatorTask) Execute() error {
	return t.executor.RemoveValidator(t.publicKey)
}

type LiquidateClusterTask struct {
	executor    TaskExecutor
	owner       common.Address
	operatorIDs []uint64
	toLiquidate []*ssvtypes.SSVShare
}

func NewLiquidateClusterTask(executor TaskExecutor, owner common.Address, operatorIDs []uint64, toLiquidate []*ssvtypes.SSVShare) *LiquidateClusterTask {
	return &LiquidateClusterTask{
		executor:    executor,
		owner:       owner,
		operatorIDs: operatorIDs,
		toLiquidate: toLiquidate,
	}
}

func (t LiquidateClusterTask) Execute() error {
	return t.executor.LiquidateCluster(t.owner, t.operatorIDs, t.toLiquidate)
}

type ReactivateClusterTask struct {
	executor    TaskExecutor
	owner       common.Address
	operatorIDs []uint64
	toEnable    []*ssvtypes.SSVShare
}

func NewReactivateClusterTask(executor TaskExecutor, owner common.Address, operatorIDs []uint64, toEnable []*ssvtypes.SSVShare) *ReactivateClusterTask {
	return &ReactivateClusterTask{
		executor:    executor,
		owner:       owner,
		operatorIDs: operatorIDs,
		toEnable:    toEnable,
	}
}

func (t ReactivateClusterTask) Execute() error {
	return t.executor.ReactivateCluster(t.owner, t.operatorIDs, t.toEnable)
}

type FeeRecipientTask struct {
	executor  TaskExecutor
	owner     common.Address
	recipient common.Address
}

func NewFeeRecipientTask(executor TaskExecutor, owner, recipient common.Address) *FeeRecipientTask {
	return &FeeRecipientTask{
		executor:  executor,
		owner:     owner,
		recipient: recipient,
	}
}

func (t FeeRecipientTask) Execute() error {
	return t.executor.UpdateFeeRecipient(t.owner, t.recipient)
}
