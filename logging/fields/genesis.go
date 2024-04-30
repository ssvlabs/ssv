// genesis.go is DEPRECATED

package fields

import (
	"fmt"
	"strings"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	"go.uber.org/zap"
)

// GenesisFormatDutyID is DEPRECATED
func GenesisFormatDutyID(epoch phase0.Epoch, duty *genesisspectypes.Duty) string {
	return fmt.Sprintf("%v-e%v-s%v-v%v", duty.Type.String(), epoch, duty.Slot, duty.ValidatorIndex)
}

// GenesisDuties is DEPRECATED
func GenesisDuties(epoch phase0.Epoch, duties []*genesisspectypes.Duty) zap.Field {
	var b strings.Builder
	for i, duty := range duties {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(GenesisFormatDutyID(epoch, duty))
	}
	return zap.String(FieldDuties, b.String())
}
