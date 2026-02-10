//go:build alan_spec

package qbft

import spectypes "github.com/ssvlabs/ssv-spec/types"

func adjustExpectedErrorCode(code int) int {
	// Alan fixtures use v1.2.2 error-code numbering. v1.2.3 removed two enum
	// members after code 9, so most legacy codes shift down by 2.
	switch code {
	case 10:
		return spectypes.ValidatorRegistrationNoConsensusPhaseErrorCode
	case 11:
		return spectypes.ValidatorExitNoConsensusPhaseErrorCode
	}

	if code >= 12 && code <= 79 {
		return code - 2
	}

	return code
}
