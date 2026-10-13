package runner

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

// A pre-consensus partial for a slot the runner has not reached yet is retryable — the duty queue
// replays it until the duty for that slot starts — while one for a slot already behind the runner is
// a plain error. Message validation admits such partials up to its early margin (issue #3026), so this
// is what keeps them from being lost.
func TestValidatePartialSigMsg_SlotAgainstDuty(t *testing.T) {
	b := &BaseRunner{}
	const dutySlot = phase0.Slot(100)
	msgs := func(slot phase0.Slot) *spectypes.PartialSignatureMessages {
		return &spectypes.PartialSignatureMessages{
			Type:     spectypes.RandaoPartialSig,
			Slot:     slot,
			Messages: []*spectypes.PartialSignatureMessage{{Signer: 1, ValidatorIndex: 1}},
		}
	}
	code := func(err error) int {
		var specErr *spectypes.Error
		require.ErrorAs(t, err, &specErr)
		return specErr.Code
	}

	err := b.validatePartialSigMsg(msgs(dutySlot+1), dutySlot)
	require.True(t, IsRetryable(err))
	require.Equal(t, spectypes.PartialSigMessageFutureSlotErrorCode, code(err))
	require.ErrorContains(t, err, ErrFuturePartialSigMsg.Error())

	err = b.validatePartialSigMsg(msgs(dutySlot-1), dutySlot)
	require.False(t, IsRetryable(err))
	require.Equal(t, spectypes.PartialSigMessageInvalidSlotErrorCode, code(err))
}
