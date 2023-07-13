package eventdatahandler

import (
	ethcommon "github.com/ethereum/go-ethereum/common"

	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

type Task interface {
	Execute() error
}

type addValidatorExecutor interface {
	AddValidator(publicKey []byte) error
}

type AddValidatorTask struct {
	executor  addValidatorExecutor
	publicKey []byte
}

func NewAddValidatorTask(executor addValidatorExecutor, publicKey []byte) *AddValidatorTask {
	return &AddValidatorTask{
		executor:  executor,
		publicKey: publicKey,
	}
}

func (t AddValidatorTask) Execute() error {
	return t.executor.AddValidator(t.publicKey)
}

type removeValidatorExecutor interface {
	RemoveValidator(publicKey []byte) error
}

type RemoveValidatorTask struct {
	executor  removeValidatorExecutor
	publicKey []byte
}

func NewRemoveValidatorTask(executor removeValidatorExecutor, publicKey []byte) *RemoveValidatorTask {
	return &RemoveValidatorTask{
		executor:  executor,
		publicKey: publicKey,
	}
}

func (t RemoveValidatorTask) Execute() error {
	return t.executor.RemoveValidator(t.publicKey)
}

type liquidateClusterExecutor interface {
	LiquidateCluster(owner ethcommon.Address, operatorIDs []uint64, toLiquidate []*ssvtypes.SSVShare) error
}

type LiquidateClusterTask struct {
	executor    liquidateClusterExecutor
	owner       ethcommon.Address
	operatorIDs []uint64
	toLiquidate []*ssvtypes.SSVShare
}

func NewLiquidateClusterTask(
	executor liquidateClusterExecutor,
	owner ethcommon.Address,
	operatorIDs []uint64,
	toLiquidate []*ssvtypes.SSVShare,
) *LiquidateClusterTask {
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

type reactivateClusterExecutor interface {
	ReactivateCluster(owner ethcommon.Address, operatorIDs []uint64, toEnable []*ssvtypes.SSVShare) error
}

type ReactivateClusterTask struct {
	executor    reactivateClusterExecutor
	owner       ethcommon.Address
	operatorIDs []uint64
	toEnable    []*ssvtypes.SSVShare
}

func NewReactivateClusterTask(
	executor reactivateClusterExecutor,
	owner ethcommon.Address,
	operatorIDs []uint64,
	toEnable []*ssvtypes.SSVShare,
) *ReactivateClusterTask {
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

type updateFeeRecipientExecutor interface {
	UpdateFeeRecipient(owner, feeRecipient ethcommon.Address) error
}

type FeeRecipientTask struct {
	executor  updateFeeRecipientExecutor
	owner     ethcommon.Address
	recipient ethcommon.Address
}

func NewFeeRecipientTask(executor updateFeeRecipientExecutor, owner, recipient ethcommon.Address) *FeeRecipientTask {
	return &FeeRecipientTask{
		executor:  executor,
		owner:     owner,
		recipient: recipient,
	}
}

func (t FeeRecipientTask) Execute() error {
	return t.executor.UpdateFeeRecipient(t.owner, t.recipient)
}
