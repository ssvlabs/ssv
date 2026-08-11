package validation

import (
	"testing"

	"github.com/ssvlabs/ssv-spec/types/spectest/tests/maxmsgsize"
	"github.com/stretchr/testify/require"
)

// specV122MaxSizePartialSignatureMessages mirrors pre-boole ssv-spec v1.2.2's
// maxmsgsize.maxSizePartialSignatureMessages (1512 messages). It is unexported there and
// only one spec version can be pinned, so the value is hardcoded here to guard the
// pre-fork cap.
const specV122MaxSizePartialSignatureMessages = 217748

// TestSizeCapsCoverSpecWorstCase guards against our hand-computed size caps
// drifting below the pinned ssv-spec's worst-case message sizes. If this
// fails after a spec bump, re-derive the corresponding const.go values.
func TestSizeCapsCoverSpecWorstCase(t *testing.T) {
	// The post-fork cap is compared against the spec's full-SSVMessage-envelope constant,
	// which is over-conservative: the cap applies to SSVMessage.Data, the inner encoding.
	// The pre-fork guard below compares against the inner v1.2.2 constant instead — the
	// only partial-signature size constant that spec version published.
	require.GreaterOrEqual(t, maxEncodedPartialSignatureSize, maxmsgsize.MaxSizeSSVMessageFromPartialSignatureMessages)
	require.GreaterOrEqual(t, maxEncodedConsensusMsgSize, maxmsgsize.MaxSizeSSVMessageFromQBFTMessage)
	require.GreaterOrEqual(t, MaxEncodedMsgSize, maxmsgsize.MaxSizeSignedSSVMessageFromQBFTWith2Justification)

	// The pre-fork cap must cover the pre-boole spec's structural worst case but stay
	// below the post-fork cap (otherwise the fork-aware switch would be pointless).
	require.GreaterOrEqual(t, preForkMaxEncodedPartialSignatureSize, specV122MaxSizePartialSignatureMessages)
	require.Less(t, preForkMaxEncodedPartialSignatureSize, maxEncodedPartialSignatureSize)
}
