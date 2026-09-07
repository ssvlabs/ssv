package faultnet

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/qa/faults"
)

// Task 8 adds tests that decode QBFT bodies; it adds the specqbft import then.

// partialMsg builds a one-entry partial-signature message for the given role and slot, the shape
// every wire fault starts from.
func partialMsg(t *testing.T, role spectypes.RunnerRole, slot phase0.Slot) *spectypes.SignedSSVMessage {
	t.Helper()

	var pk spectypes.ValidatorPK
	pk[0] = 0xab

	body := &spectypes.PartialSignatureMessages{
		Type: spectypes.PTCAttesterPartialSig,
		Slot: slot,
		Messages: []*spectypes.PartialSignatureMessage{{
			PartialSignature: make([]byte, 96), // spectypes.Signature is []byte, ssz-size 96
			SigningRoot:      [32]byte{0x01},
			Signer:           7,
			ValidatorIndex:   42,
		}},
	}
	data, err := body.Encode()
	require.NoError(t, err)

	return &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{{0x09}},
		OperatorIDs: []spectypes.OperatorID{7},
		SSVMessage: &spectypes.SSVMessage{
			MsgType: spectypes.SSVPartialSignatureMsgType,
			MsgID:   spectypes.NewValidatorMsgID(spectypes.DomainType{0x00, 0x00, 0x31, 0x14}, pk, role),
			Data:    data,
		},
	}
}

func TestPlanPassesThroughWhenNoFault(t *testing.T) {
	msg := partialMsg(t, spectypes.RolePTCAttester, 100)

	out := Plan(faults.None, msg, 100, 100)

	require.Len(t, out, 1)
	require.Same(t, msg, out[0].Msg, "the identity path must not copy the message")
	require.False(t, out[0].Resign)
	require.Zero(t, out[0].Delay)
	require.Equal(t, phase0.Slot(100), out[0].Slot)
}

func TestPlanLeavesUnrelatedMessagesAlone(t *testing.T) {
	// two-entries targets role 7 partials; a role-0 committee message must pass straight through.
	msg := partialMsg(t, spectypes.RoleCommittee, 100)

	out := Plan(faults.TwoEntries, msg, 100, 100)

	require.Len(t, out, 1)
	require.Same(t, msg, out[0].Msg)
	require.False(t, out[0].Resign)
}

func TestCloneIsIndependent(t *testing.T) {
	msg := partialMsg(t, spectypes.RolePTCAttester, 100)

	c, err := Clone(msg)
	require.NoError(t, err)
	require.NotSame(t, msg, c)

	c.SSVMessage.Data[0] ^= 0xff
	require.NotEqual(t, msg.SSVMessage.Data[0], c.SSVMessage.Data[0])
}
