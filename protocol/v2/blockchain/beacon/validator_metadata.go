package beacon

import (
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

//go:generate mockgen -package=beacon -destination=./mock_validator_metadata.go -source=./validator_metadata.go

// ValidatorMetadata represents validator metdata from beacon
type ValidatorMetadata struct {
	Balance         phase0.Gwei              `json:"balance"`
	Status          eth2apiv1.ValidatorState `json:"status"`
	Index           phase0.ValidatorIndex    `json:"index"` // pointer in order to support nil
	ActivationEpoch phase0.Epoch             `json:"activation_epoch"`
}

// Equals returns true if the given metadata is equal to current
func (m *ValidatorMetadata) Equals(other *ValidatorMetadata) bool {
	return m != nil &&
		other != nil &&
		*m == *other
}

// Pending returns true if the validator is pending
func (m *ValidatorMetadata) Pending() bool {
	return m.Status.IsPending()
}

// Activated returns true if the validator is not unknown. It might be pending activation or active
func (m *ValidatorMetadata) Activated() bool {
	return m.Status.HasActivated() || m.Status.IsActive() || m.Status.IsAttesting()
}

// IsActive returns true if the validator is currently active. Cant be other state
func (m *ValidatorMetadata) IsActive() bool {
	return m.Status == eth2apiv1.ValidatorStateActiveOngoing
}

// IsAttesting returns true if the validator should be attesting.
func (m *ValidatorMetadata) IsAttesting() bool {
	return m.Status.IsAttesting()
}

// Exiting returns true if the validator is existing or exited
func (m *ValidatorMetadata) Exiting() bool {
	return m.Status.IsExited() || m.Status.HasExited()
}

// Slashed returns true if the validator is existing or exited due to slashing
func (m *ValidatorMetadata) Slashed() bool {
	return m.Status == eth2apiv1.ValidatorStateExitedSlashed || m.Status == eth2apiv1.ValidatorStateActiveSlashed
}
