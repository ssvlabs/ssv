package eventhandler

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
	ethcommon "github.com/ethereum/go-ethereum/common"

	"github.com/bloxapp/ssv/protocol/v2/types"
)

type Task interface {
	Execute() error
}

type startValidatorExecutor interface {
	StartValidator(share *types.SSVShare) error
}

type StartValidatorTask struct {
	executor startValidatorExecutor
	share    *types.SSVShare
}

func NewStartValidatorTask(executor startValidatorExecutor, share *types.SSVShare) *StartValidatorTask {
	return &StartValidatorTask{
		executor: executor,
		share:    share,
	}
}

func (t StartValidatorTask) Execute() error {
	return t.executor.StartValidator(t.share)
}

type stopValidatorExecutor interface {
	StopValidator(pubKey spectypes.ValidatorPK) error
}

type StopValidatorTask struct {
	executor stopValidatorExecutor
	pubKey   spectypes.ValidatorPK
}

func NewStopValidatorTask(executor stopValidatorExecutor, pubKey spectypes.ValidatorPK) *StopValidatorTask {
	return &StopValidatorTask{
		executor: executor,
		pubKey:   pubKey,
	}
}

func (t StopValidatorTask) Execute() error {
	return t.executor.StopValidator(t.pubKey)
}

type liquidateClusterExecutor interface {
	LiquidateCluster(owner ethcommon.Address, operatorIDs []spectypes.OperatorID, toLiquidate []*types.SSVShare) error
}

type LiquidateClusterTask struct {
	executor    liquidateClusterExecutor
	owner       ethcommon.Address
	operatorIDs []spectypes.OperatorID
	toLiquidate []*types.SSVShare
}

func NewLiquidateClusterTask(
	executor liquidateClusterExecutor,
	owner ethcommon.Address,
	operatorIDs []spectypes.OperatorID,
	toLiquidate []*types.SSVShare,
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
	ReactivateCluster(owner ethcommon.Address, operatorIDs []spectypes.OperatorID, toReactivate []*types.SSVShare) error
}

type ReactivateClusterTask struct {
	executor     reactivateClusterExecutor
	owner        ethcommon.Address
	operatorIDs  []spectypes.OperatorID
	toReactivate []*types.SSVShare
}

func NewReactivateClusterTask(
	executor reactivateClusterExecutor,
	owner ethcommon.Address,
	operatorIDs []spectypes.OperatorID,
	toReactivate []*types.SSVShare,
) *ReactivateClusterTask {
	return &ReactivateClusterTask{
		executor:     executor,
		owner:        owner,
		operatorIDs:  operatorIDs,
		toReactivate: toReactivate,
	}
}

func (t ReactivateClusterTask) Execute() error {
	return t.executor.ReactivateCluster(t.owner, t.operatorIDs, t.toReactivate)
}

type updateFeeRecipientExecutor interface {
	UpdateFeeRecipient(owner, feeRecipient ethcommon.Address) error
}

type UpdateFeeRecipientTask struct {
	executor  updateFeeRecipientExecutor
	owner     ethcommon.Address
	recipient ethcommon.Address
}

func NewUpdateFeeRecipientTask(executor updateFeeRecipientExecutor, owner, recipient ethcommon.Address) *UpdateFeeRecipientTask {
	return &UpdateFeeRecipientTask{
		executor:  executor,
		owner:     owner,
		recipient: recipient,
	}
}

func (t UpdateFeeRecipientTask) Execute() error {
	return t.executor.UpdateFeeRecipient(t.owner, t.recipient)
}
