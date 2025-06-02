package storage

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/types"
)

// ValidatorStore is the authoritative source for all validator state management.
// It handles all state transitions and provides thread-safe access to validator data.
type ValidatorStore interface {
	OnShareAdded(ctx context.Context, share *types.SSVShare) error
	OnShareUpdated(ctx context.Context, share *types.SSVShare) error
	OnShareRemoved(ctx context.Context, pubKey spectypes.ValidatorPK) error
	OnClusterLiquidated(ctx context.Context, owner common.Address, operatorIDs []uint64) error
	OnClusterReactivated(ctx context.Context, owner common.Address, operatorIDs []uint64) error
	OnFeeRecipientUpdated(ctx context.Context, owner common.Address, recipient common.Address) error
	OnValidatorExited(ctx context.Context, pubKey spectypes.ValidatorPK, blockNumber uint64) error
	OnOperatorRemoved(ctx context.Context, operatorID spectypes.OperatorID) error

	RegisterLifecycleCallbacks(callbacks ValidatorLifecycleCallbacks)

	GetValidator(id ValidatorID) (*ValidatorSnapshot, bool)
	GetCommittee(id spectypes.CommitteeID) (*CommitteeSnapshot, bool)
	GetAllValidators() []*ValidatorSnapshot
	GetOperatorValidators(operatorID spectypes.OperatorID) []*ValidatorSnapshot
	GetParticipatingValidators(epoch phase0.Epoch, opts ParticipationOptions) []*ValidatorSnapshot
	GetCommittees() []*CommitteeSnapshot
	GetOperatorCommittees(operatorID spectypes.OperatorID) []*CommitteeSnapshot

	RegisterSyncCommitteeInfo(info []SyncCommitteeInfo) error
	GetSyncCommitteeValidators(period uint64) []*ValidatorSnapshot
}

// ParticipationOptions filters participating validators.
type ParticipationOptions struct {
	IncludeLiquidated bool
	IncludeExited     bool
	OnlyAttesting     bool
	OnlySyncCommittee bool
}
