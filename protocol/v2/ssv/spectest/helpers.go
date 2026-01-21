package spectest

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
)

func keySetFromShares(shares map[phase0.ValidatorIndex]*spectypes.Share) *spectestingutils.TestKeySet {
	for _, share := range shares {
		return spectestingutils.KeySetForShare(share)
	}
	return nil
}

func mapForKeys(m map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if value, ok := m[key]; ok {
			if cast, ok := value.(map[string]any); ok {
				return cast
			}
		}
	}
	return nil
}
