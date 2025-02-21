package types

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

type BlockRootWithSlot struct {
	spectypes.SSZBytes // block root
	Slot               phase0.Slot
}
