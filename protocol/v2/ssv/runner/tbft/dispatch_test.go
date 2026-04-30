package tbft

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	tbftcore "github.com/ssvlabs/ssv/protocol/v2/tbft"
	"github.com/ssvlabs/ssv/protocol/v2/tbft/wire"
)

func TestDispatchEnvelope_NilArgs(t *testing.T) {
	c := newStubController(t, 7)
	require.ErrorContains(t, DispatchEnvelope(nil, &wire.Envelope{}), "nil Controller")
	require.ErrorContains(t, DispatchEnvelope(c, nil), "nil Envelope")
}

func TestDispatchEnvelope_RoutesByKind(t *testing.T) {
	c := newStubController(t, 7)
	_, err := c.StartNewInstance(phase0.Slot(50))
	require.NoError(t, err)

	t.Run("KindOnion", func(t *testing.T) {
		env := &wire.Envelope{
			Kind: wire.KindOnion,
			Onion: &tbftcore.Onion{
				OperatorID: 1,
				Height:     50,
				Layers:     make([]tbftcore.EncryptedLayer, 3),
			},
		}
		require.NoError(t, DispatchEnvelope(c, env))
	})

	t.Run("KindNonReceipt", func(t *testing.T) {
		env := &wire.Envelope{
			Kind: wire.KindNonReceipt,
			NonReceipt: &tbftcore.NonReceiptAttestation{
				OperatorID: 2,
				Height:     50,
				Layer:      0,
				PartialSig: tbftcore.Signature("p"),
			},
		}
		require.NoError(t, DispatchEnvelope(c, env))
	})

	t.Run("KindCandidate", func(t *testing.T) {
		env := &wire.Envelope{
			Kind: wire.KindCandidate,
			Candidate: &tbftcore.CandidateBroadcast{
				OperatorID: 3,
				Height:     50,
				Layer:      0,
				Value:      tbftcore.Value("v"),
			},
		}
		require.NoError(t, DispatchEnvelope(c, env))
	})
}

func TestDispatchEnvelope_UnknownKind(t *testing.T) {
	c := newStubController(t, 7)
	env := &wire.Envelope{Kind: wire.MessageKind(0xFE)}
	err := DispatchEnvelope(c, env)
	require.ErrorContains(t, err, "unknown envelope kind")
}

func TestDispatchEnvelope_PropagatesProcessError(t *testing.T) {
	// Onion targeting a slot with no instance should propagate the
	// "no active instance" error.
	c := newStubController(t, 7)
	env := &wire.Envelope{
		Kind: wire.KindOnion,
		Onion: &tbftcore.Onion{
			OperatorID: 1,
			Height:     999,
			Layers:     make([]tbftcore.EncryptedLayer, 3),
		},
	}
	err := DispatchEnvelope(c, env)
	require.ErrorContains(t, err, "no active instance for slot 999")
}

func TestDispatchBytes_HappyPath(t *testing.T) {
	c := newStubController(t, 7)
	_, err := c.StartNewInstance(phase0.Slot(7))
	require.NoError(t, err)

	original := &tbftcore.CandidateBroadcast{
		OperatorID: 5,
		Height:     7,
		Layer:      0,
		Value:      tbftcore.Value("payload"),
	}
	bytes_, err := wire.WrapCandidate(original)
	require.NoError(t, err)

	require.NoError(t, DispatchBytes(c, bytes_))
}

func TestDispatchBytes_MalformedRejected(t *testing.T) {
	c := newStubController(t, 7)
	require.Error(t, DispatchBytes(c, []byte{0xFF})) // bad version
	require.Error(t, DispatchBytes(c, nil))          // empty
}
