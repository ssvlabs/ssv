package validation

import (
	"testing"

	"github.com/ssvlabs/ssv-spec/types/spectest/tests/maxmsgsize"
	"github.com/stretchr/testify/require"
)

// TestSizeCapsCoverSpecWorstCase guards against our hand-computed size caps
// drifting below the pinned ssv-spec's worst-case message sizes. If this
// fails after a spec bump, re-derive the corresponding const.go values.
func TestSizeCapsCoverSpecWorstCase(t *testing.T) {
	require.GreaterOrEqual(t, maxEncodedPartialSignatureSize, maxmsgsize.MaxSizeSSVMessageFromPartialSignatureMessages)
	require.GreaterOrEqual(t, maxEncodedConsensusMsgSize, maxmsgsize.MaxSizeSSVMessageFromQBFTMessage)
	require.GreaterOrEqual(t, MaxEncodedMsgSize, maxmsgsize.MaxSizeSignedSSVMessageFromQBFTWith2Justification)
}
