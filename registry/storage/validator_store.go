package storage

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"

	"github.com/ssvlabs/ssv/protocol/v2/types"
)

//go:generate go tool -modfile=../../tool.mod mockgen -package=mocks -destination=./mocks/validator_store.go -source=./validator_store.go

// UpdateOptions controls behavior of state updates.
type UpdateOptions struct {
	TriggerCallbacks bool
}

// ValidatorStore is the authoritative source for all validator state management.
// It handles all state transitions and provides thread-safe access to validator data.
type ValidatorStore interface {
	OnShareAdded(ctx context.Context, share *types.SSVShare, opts UpdateOptions) error
	OnShareUpdated(ctx context.Context, share *types.SSVShare, opts UpdateOptions) error
	OnShareRemoved(ctx context.Context, pubKey spectypes.ValidatorPK, opts UpdateOptions) error
	OnClusterLiquidated(ctx context.Context, owner common.Address, operatorIDs []uint64, opts UpdateOptions) error
	OnClusterReactivated(ctx context.Context, owner common.Address, operatorIDs []uint64, opts UpdateOptions) error
	OnFeeRecipientUpdated(ctx context.Context, owner common.Address, recipient common.Address, opts UpdateOptions) error
	OnValidatorExited(ctx context.Context, pubKey spectypes.ValidatorPK, blockNumber uint64, opts UpdateOptions) error
	OnOperatorRemoved(ctx context.Context, operatorID spectypes.OperatorID, opts UpdateOptions) error

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

	// UpdateValidatorsMetadata updates the metadata for multiple validators.
	// Returns only the metadata that actually changed the stored shares.
	UpdateValidatorsMetadata(ctx context.Context, metadata beacon.ValidatorMetadataMap) (beacon.ValidatorMetadataMap, error)

	GetSelfValidators() []*ValidatorSnapshot
	GetSelfParticipatingValidators(epoch phase0.Epoch, opts ParticipationOptions) []*ValidatorSnapshot
}

// ParticipationOptions filters participating validators.
type ParticipationOptions struct {
	IncludeLiquidated bool
	IncludeExited     bool
	OnlyAttesting     bool
	OnlySyncCommittee bool
}
