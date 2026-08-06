package runner

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/ssv"
)

// TestResolveDuplicateSignatureUnheldValidatorIndex is the regression guard for the nil-share
// panic: committee-role message validation deliberately does not assert that a partial-signature
// validator index belongs to a validator this operator holds a share for (knowledge-base#2), so a
// duplicate partial signature carrying an unheld index reaches resolveDuplicateSignature, where the
// old code dereferenced b.Share[idx].Committee on a nil *Share and crashed the whole node.
//
// The runner holds a share for index 1 only; the message targets index 99. The call must not panic
// and must drop any stored entry for the unheld index rather than verifying against a missing share.
func TestResolveDuplicateSignatureUnheldValidatorIndex(t *testing.T) {
	t.Parallel()

	const heldIndex = phase0.ValidatorIndex(1)
	const unheldIndex = phase0.ValidatorIndex(99)

	runner := &BaseRunner{
		Share: map[phase0.ValidatorIndex]*spectypes.Share{
			heldIndex: {ValidatorIndex: heldIndex},
		},
	}

	root := [32]byte{0xAB}
	msg := &spectypes.PartialSignatureMessage{
		PartialSignature: make([]byte, 96),
		SigningRoot:      root,
		Signer:           2,
		ValidatorIndex:   unheldIndex,
	}

	container := ssv.NewPartialSigContainer(3)
	// Seed the entry so resolveDuplicateSignature has something to resolve (mirrors the duplicate
	// path in basePartialSigMsgProcessing, which only calls this when HasSignature is already true).
	container.AddSignature(msg)
	require.True(t, container.HasSignature(unheldIndex, msg.Signer, root))

	require.NotPanics(t, func() {
		runner.resolveDuplicateSignature(container, msg)
	})

	// The unverifiable entry for the unheld index is dropped.
	require.False(t, container.HasSignature(unheldIndex, msg.Signer, root))
}
