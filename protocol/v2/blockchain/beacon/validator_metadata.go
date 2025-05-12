package beacon

import (
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// ValidatorMetadata represents validator metadata from Ethereum beacon node
type ValidatorMetadata struct {
	Status          eth2apiv1.ValidatorState `json:"status"`
	Index           phase0.ValidatorIndex    `json:"index"`
	ActivationEpoch phase0.Epoch             `json:"activation_epoch"`
	ExitEpoch       phase0.Epoch             `json:"exit_epoch"`
}

// Equals returns true if the given metadata is equal to current
func (m *ValidatorMetadata) Equals(other *ValidatorMetadata) bool {
	return other != nil &&
		m.Status == other.Status &&
		m.Index == other.Index &&
		m.ActivationEpoch == other.ActivationEpoch &&
		m.ExitEpoch == other.ExitEpoch
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

// Slashed returns true if the validator is exiting or exited due to slashing
func (m *ValidatorMetadata) Slashed() bool {
	return m.Status == eth2apiv1.ValidatorStateExitedSlashed || m.Status == eth2apiv1.ValidatorStateActiveSlashed
}
