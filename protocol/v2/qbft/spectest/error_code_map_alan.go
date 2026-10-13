//go:build alan_spec

package qbft

import spectypes "github.com/ssvlabs/ssv-spec/types"

// maxShiftableLegacyErrorCode is the highest v1.2.2 error code eligible for the -2 shift below.
// spectypes.TimeoutInstanceErrorCode is the last error code that existed in the spec before the
// Boole-era additions (AggComm*/Committee* codes), which have no v1.2.2 counterpart and thus were
// never subject to the shift. Its v1.2.2 value is its current (v1.2.3+) value plus the 2-code
// offset described below: 70 + 2 = 72.
const maxShiftableLegacyErrorCode = int(spectypes.TimeoutInstanceErrorCode) + 2

func adjustExpectedErrorCode(code int) int {
	// Alan fixtures use v1.2.2 error-code numbering. v1.2.3 removed two enum
	// members after code 9, so most legacy codes shift down by 2.
	switch code {
	case 10:
		return spectypes.ValidatorRegistrationNoConsensusPhaseErrorCode
	case 11:
		return spectypes.ValidatorExitNoConsensusPhaseErrorCode
	}

	if code >= 12 && code <= maxShiftableLegacyErrorCode {
		return code - 2
	}

	return code
}
