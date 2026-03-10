package auditor

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	registrystorage "github.com/ssvlabs/ssv/registry/storage"
)

// buildValidatorToCommitteeIndex creates a reverse lookup map from validator index to committee ID.
// Committees here are SSV operator clusters (not beacon committees).
func buildValidatorToCommitteeIndex(committees []*registrystorage.Committee) map[phase0.ValidatorIndex]spectypes.CommitteeID {
	result := make(map[phase0.ValidatorIndex]spectypes.CommitteeID)
	for _, cmt := range committees {
		if cmt == nil {
			continue
		}
		for _, validatorIndex := range cmt.Indices {
			result[validatorIndex] = cmt.ID
		}
	}
	return result
}
