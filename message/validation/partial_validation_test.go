package validation

import (
	"bytes"
	"context"
	"testing"
	"time"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/networkconfig"
)

// TestPartialSignatureSizeCapEnforcedInValidation drives validatePartialSignatureMessage
// itself (not just the constant) with payloads around the cap, guarding that the size gate
// stays wired into the validation path: an oversized payload is rejected as too big, while
// one at the cap passes the size gate and only fails later, at decoding.
func TestPartialSignatureSizeCapEnforcedInValidation(t *testing.T) {
	t.Parallel()

	mv := &messageValidator{netCfg: networkconfig.TestNetwork}

	t.Run("payload above the cap is rejected", func(t *testing.T) {
		t.Parallel()

		signedSSVMessage := &spectypes.SignedSSVMessage{
			SSVMessage: &spectypes.SSVMessage{Data: bytes.Repeat([]byte{1}, maxEncodedPartialSignatureSize+1)},
		}
		_, err := mv.validatePartialSignatureMessage(context.Background(), signedSSVMessage, CommitteeInfo{}, "", "", time.Time{})
		require.ErrorIs(t, err, ErrSSVDataTooBig)

		var valErr Error
		require.ErrorAs(t, err, &valErr)
		require.Equal(t, maxEncodedPartialSignatureSize, valErr.want)
	})

	t.Run("payload at the cap passes the size gate", func(t *testing.T) {
		t.Parallel()

		signedSSVMessage := &spectypes.SignedSSVMessage{
			SSVMessage: &spectypes.SSVMessage{Data: bytes.Repeat([]byte{1}, maxEncodedPartialSignatureSize)},
		}
		_, err := mv.validatePartialSignatureMessage(context.Background(), signedSSVMessage, CommitteeInfo{}, "", "", time.Time{})
		require.NotErrorIs(t, err, ErrSSVDataTooBig)
		// The garbage payload fails at the next step, decoding — proof the size gate
		// (not the content) made the difference between the two cases.
		require.ErrorIs(t, err, ErrUndecodableMessageData)
	})
}
