package types

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// SlashingProtectionData holds duty source and target epochs.
// It's used for majority fork protection and only for spectypes.BeaconVote.
type SlashingProtectionData struct {
	SourceEpoch phase0.Epoch
	TargetEpoch phase0.Epoch
	TargetRoot  phase0.Root
}
