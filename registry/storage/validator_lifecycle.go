package storage

import (
	"context"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/operator/duties"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

// ValidatorLifecycleCallbacks defines hooks for validator lifecycle events.
// All callbacks are optional and should not block.
type ValidatorLifecycleCallbacks struct {
	// OnValidatorAdded is called when a new validator is added.
	OnValidatorAdded func(ctx context.Context, share *ValidatorSnapshot) error

	// OnValidatorStarted is called when a validator should start participating.
	OnValidatorStarted func(ctx context.Context, share *ValidatorSnapshot) error

	// OnValidatorStopped is called when a validator should stop participating.
	OnValidatorStopped func(ctx context.Context, pubKey spectypes.ValidatorPK) error

	// OnValidatorUpdated is called when validator metadata changes.
	OnValidatorUpdated func(ctx context.Context, share *ValidatorSnapshot) error

	// OnValidatorRemoved is called when a validator is permanently removed.
	OnValidatorRemoved func(ctx context.Context, pubKey spectypes.ValidatorPK) error

	// OnValidatorExited is called when a voluntary exit is initiated.
	OnValidatorExited func(ctx context.Context, descriptor duties.ExitDescriptor) error

	// OnCommitteeChanged is called when committee membership changes.
	OnCommitteeChanged func(ctx context.Context, committeeID spectypes.CommitteeID, action CommitteeAction) error

	// OnIndicesChanged is called when validator indices require re-fetching duties.
	OnIndicesChanged func(ctx context.Context) error
}

// CommitteeAction describes what happened to a committee.
type CommitteeAction string

const (
	CommitteeActionCreated CommitteeAction = "created"
	CommitteeActionUpdated CommitteeAction = "updated"
	CommitteeActionRemoved CommitteeAction = "removed"
)

// ValidatorSnapshot is an immutable view of a validator's state.
// It's safe to use across goroutines as it contains no mutable fields.
type ValidatorSnapshot struct {
	Share               types.SSVShare
	LastUpdated         time.Time
	IsOwnValidator      bool
	ParticipationStatus ParticipationStatus
}

// ParticipationStatus tracks why a validator is or isn't participating.
type ParticipationStatus struct {
	IsParticipating     bool
	IsLiquidated        bool
	HasBeaconMetadata   bool
	IsAttesting         bool
	IsSyncCommittee     bool
	MinParticipationMet bool
	Reason              string
}

// CommitteeSnapshot is an immutable view of a committee.
type CommitteeSnapshot struct {
	ID         spectypes.CommitteeID
	Operators  []spectypes.OperatorID
	Validators []*ValidatorSnapshot
}

// SyncCommitteeInfo tracks sync committee participation.
type SyncCommitteeInfo struct {
	ValidatorIndex phase0.ValidatorIndex
	Period         uint64
	Indices        []phase0.CommitteeIndex
}
