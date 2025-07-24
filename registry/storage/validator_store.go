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

// ValidatorStore serves as the single source of truth for all validator-related states within the SSV node.
// It provides a thread-safe, in-memory cache of validator and committee information, abstracting the underlying
// persistence layer (e.g., SharesStorage). The store is responsible for managing the complete lifecycle of
// validators, from their addition and activation to their removal or liquidation.
//
// It is designed with a clear separation of concerns:
//   - ValidatorStateManager: For all state-mutating operations.
//   - ValidatorQuerier and CommitteeQuerier: For all read-only data access.
//   - Lifecycle Callbacks: To allow other components to react to state changes in a decoupled manner.
type ValidatorStore interface {
	ValidatorStateManager
	ValidatorQuerier
	CommitteeQuerier
	ValidatorReporter

	// RegisterLifecycleCallbacks registers a set of callback functions that are invoked in response to
	// specific validator lifecycle events, such as a validator being added, updated, or removed.
	// This enables a reactive, event-driven architecture where other components (e.g., duty runners,
	// metrics exporters) can subscribe to state changes without being tightly coupled to the store's
	// implementation. Callbacks are executed asynchronously and should be non-blocking.
	RegisterLifecycleCallbacks(callbacks ValidatorLifecycleCallbacks)
}

// ValidatorStateManager defines the interface for all state-mutating operations on the validator store.
type ValidatorStateManager interface {
	// OnShareAdded handles the addition of a new validator share. This is a critical event that
	// introduces a new validator to the node's management. The implementation should validate the share,
	// update the internal state for the validator and its committee, persist the share, and trigger
	// the OnValidatorAdded callback if specified in the options.
	OnShareAdded(ctx context.Context, share *types.SSVShare, opts UpdateOptions) error

	// OnShareUpdated processes updates to an existing validator share. This can occur due to changes
	// in the cluster configuration or beacon chain state. The implementation updates the validator's
	// snapshot, persists the changes, and triggers relevant callbacks (e.g., OnValidatorUpdated,
	// OnValidatorStarted/Stopped) based on the nature of the update.
	OnShareUpdated(ctx context.Context, share *types.SSVShare, opts UpdateOptions) error

	// OnShareRemoved handles the removal of a validator share. This action effectively removes the
	// validator from the node's active management. The implementation must clean up all associated
	// states, including committee memberships and persisted storage, and trigger the OnValidatorRemoved callback.
	OnShareRemoved(ctx context.Context, pubKey spectypes.ValidatorPK, opts UpdateOptions) error

	// OnClusterLiquidated processes the liquidation of an entire validator cluster. A cluster is identified
	// by its owner address and the set of operator IDs. This is a severe penalty event. The implementation
	// marks all validators in the cluster as liquidated, stops their participation, and triggers callbacks.
	OnClusterLiquidated(ctx context.Context, owner common.Address, operatorIDs []uint64, opts UpdateOptions) error

	// OnClusterReactivated handles the reactivation of a previously liquidated cluster. This reverses
	// the effects of liquidation, allowing validators in the cluster to resume their duties.
	OnClusterReactivated(ctx context.Context, owner common.Address, operatorIDs []uint64, opts UpdateOptions) error

	// OnFeeRecipientUpdated processes an update to the fee recipient address for all validators
	// associated with a given owner. This address determines where execution-layer rewards are sent.
	OnFeeRecipientUpdated(ctx context.Context, owner common.Address, recipient common.Address, opts UpdateOptions) error

	// OnValidatorExited handles the event where a validator has initiated a voluntary exit from the
	// beacon chain. This is a terminal state for a validator's active lifecycle.
	OnValidatorExited(ctx context.Context, pubKey spectypes.ValidatorPK, blockNumber uint64, opts UpdateOptions) error

	// OnOperatorRemoved handles the event where an operator is removed from the SSV network. This action
	// affects all validators that include the given operator in their committee, typically leading to
	// their removal from the node's management.
	OnOperatorRemoved(ctx context.Context, operatorID spectypes.OperatorID, opts UpdateOptions) error

	// UpdateValidatorsMetadata applies a batch of metadata updates from the beacon chain to the
	// corresponding validators in the store. This is the primary mechanism for keeping the validator
	// state (e.g., status, index, activation/exit epochs) synchronized with the consensus layer.
	// It returns a map containing only the metadata for validators that were actually changed.
	UpdateValidatorsMetadata(ctx context.Context, metadata beacon.ValidatorMetadataMap) (beacon.ValidatorMetadataMap, error)
}

// ValidatorQuerier defines the read-only interface for accessing validator data.
// Methods provide different ways to query for individual or groups of validators,
// returning immutable snapshots to ensure thread safety.
type ValidatorQuerier interface {
	// GetValidator retrieves a single validator's snapshot by its unique identifier, which can be
	// either a ValidatorPubKey or a ValidatorIndex. Returns the snapshot and a boolean indicating
	// whether the validator was found.
	GetValidator(id ValidatorID) (*ValidatorSnapshot, bool)

	// GetValidatorIndex retrieves a validator's index by its identifier (pubkey or index).
	// This is a convenience method that extracts the index from the validator's metadata.
	// Returns the index and a boolean indicating whether the validator was found and has an index.
	// If the input is already a ValidatorIndex, it validates that the validator exists.
	GetValidatorIndex(id ValidatorID) (phase0.ValidatorIndex, bool)

	// GetAllValidators returns a slice of snapshots for all validators currently managed by the store.
	GetAllValidators() []*ValidatorSnapshot

	// GetOperatorValidators returns a slice of snapshots for all validators that a specific
	// operator is a member of.
	GetOperatorValidators(operatorID spectypes.OperatorID) []*ValidatorSnapshot

	// GetParticipatingValidators returns a slice of snapshots for validators that are currently
	// expected to be performing duties (e.g., attesting). The results can be filtered by the
	// provided ParticipationOptions.
	GetParticipatingValidators(epoch phase0.Epoch, opts ParticipationOptions) []*ValidatorSnapshot

	// GetSelfValidators returns a slice of snapshots for all validators that this specific SSV node
	// (identified by its operator ID) is a part of. This is a convenience method for
	// `GetOperatorValidators(self_operator_id)`.
	GetSelfValidators() []*ValidatorSnapshot

	// GetSelfParticipatingValidators returns a slice of snapshots for validators that this SSV node
	// is part of and that are currently expected to be performing duties. This is a convenience
	// method combining the logic of GetSelfValidators and GetParticipatingValidators.
	GetSelfParticipatingValidators(epoch phase0.Epoch, opts ParticipationOptions) []*ValidatorSnapshot
}

// CommitteeQuerier defines the read-only interface for accessing committee data.
type CommitteeQuerier interface {
	// GetCommittee retrieves a single committee's snapshot by its unique ID.
	GetCommittee(id spectypes.CommitteeID) (*CommitteeSnapshot, bool)

	// GetCommittees returns a slice of snapshots for all committees known to the store.
	GetCommittees() []*CommitteeSnapshot

	// GetOperatorCommittees returns a slice of snapshots for all committees that a specific
	// operator is a member of.
	GetOperatorCommittees(operatorID spectypes.OperatorID) []*CommitteeSnapshot
}

// ValidatorStatus represents the current status of a validator.
type ValidatorStatus string

const (
	ValidatorStatusParticipating ValidatorStatus = "participating"
	ValidatorStatusNotFound      ValidatorStatus = "not_found"
	ValidatorStatusActive        ValidatorStatus = "active"
	ValidatorStatusSlashed       ValidatorStatus = "slashed"
	ValidatorStatusExiting       ValidatorStatus = "exiting"
	ValidatorStatusNotActivated  ValidatorStatus = "not_activated"
	ValidatorStatusPending       ValidatorStatus = "pending"
	ValidatorStatusNoIndex       ValidatorStatus = "no_index"
	ValidatorStatusUnknown       ValidatorStatus = "unknown"
)

// ValidatorStatusReport contains aggregated counts of validators by status
type ValidatorStatusReport map[ValidatorStatus]uint32

// ValidatorReporter defines the interface for getting validator status reports
type ValidatorReporter interface {
	// GetValidatorStatusReport returns aggregated counts of validators by their current status.
	// This method calculates the status for all validators owned by the current operator
	// and returns a map with counts for each status type.
	GetValidatorStatusReport() ValidatorStatusReport
}
