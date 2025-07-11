package types

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

type SlashingProtectionData struct {
	SourceEpoch phase0.Epoch
	TargetEpoch phase0.Epoch
}
