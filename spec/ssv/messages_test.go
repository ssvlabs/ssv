package ssv_test

import (
	"github.com/bloxapp/ssv/spec/ssv"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSignedPostConsensusMessage_MatchedSigners(t *testing.T) {
	t.Run("matched same order", func(t *testing.T) {
		msg := &ssv.SignedPartialSignatureMessage{}
		msg.Signers = []types.OperatorID{1, 2, 3, 4}
		require.True(t, msg.MatchedSigners([]types.OperatorID{1, 2, 3, 4}))
	})

	t.Run("matched different order", func(t *testing.T) {
		msg := &ssv.SignedPartialSignatureMessage{}
		msg.Signers = []types.OperatorID{1, 2, 3, 4}
		require.True(t, msg.MatchedSigners([]types.OperatorID{2, 1, 4, 3}))
	})

	t.Run("matched same order with duplicate", func(t *testing.T) {
		msg := &ssv.SignedPartialSignatureMessage{}
		msg.Signers = []types.OperatorID{3, 1, 2, 3}
		require.True(t, msg.MatchedSigners([]types.OperatorID{3, 1, 2, 3}))
	})

	t.Run("matched different duplicate", func(t *testing.T) {
		msg := &ssv.SignedPartialSignatureMessage{}
		msg.Signers = []types.OperatorID{1, 2, 3, 3}
		require.True(t, msg.MatchedSigners([]types.OperatorID{3, 1, 2, 3}))
	})

	t.Run("not matched same order", func(t *testing.T) {
		msg := &ssv.SignedPartialSignatureMessage{}
		msg.Signers = []types.OperatorID{1, 2, 3, 4, 4}
		require.False(t, msg.MatchedSigners([]types.OperatorID{1, 2, 3, 4}))
	})

	t.Run("not matched", func(t *testing.T) {
		msg := &ssv.SignedPartialSignatureMessage{}
		msg.Signers = []types.OperatorID{1, 2, 3, 3}
		require.False(t, msg.MatchedSigners([]types.OperatorID{1, 2, 3, 4}))
	})

	t.Run("not matched", func(t *testing.T) {
		msg := &ssv.SignedPartialSignatureMessage{}
		msg.Signers = []types.OperatorID{1, 2, 3}
		require.False(t, msg.MatchedSigners([]types.OperatorID{1, 2, 3, 4}))
	})
}

//func TestSignedPostConsensusMessage_Aggregate(t *testing.T) {
//	threshold.Init()
//	sk1 := &bls.SecretKey{}
//	sk1.SetByCSPRNG()
//	sk2 := &bls.SecretKey{}
//	sk2.SetByCSPRNG()
//	sk3 := &bls.SecretKey{}
//	sk3.SetByCSPRNG()
//	sk4 := &bls.SecretKey{}
//	sk4.SetByCSPRNG()
//
//	t.Run("valid aggregate", func(t *testing.T) {
//		sig := sk1.SignByte([]byte{1, 2, 3, 4})
//		sig.Add(sk2.SignByte([]byte{1, 2, 3, 4}))
//		msg1 := &PartialSignatureMessage{
//			PartialSignature:   sk1.SignByte([]byte{1, 2, 3, 4}).Serialize(),
//			Signers:         []types.NodeID{1},
//			SigningRoot: []byte{1, 2, 3, 4},
//		}
//		msg2 := &PartialSignatureMessage{
//			PartialSignature:   sk2.SignByte([]byte{1, 2, 3, 4}).Serialize(),
//			Signers:         []types.NodeID{2},
//			SigningRoot: []byte{1, 2, 3, 4},
//		}
//
//		require.NoError(t, msg1.Aggregate(msg2))
//		msg1.MatchedSigners([]types.NodeID{1, 2})
//	})
//
//	t.Run("partially matching Signers", func(t *testing.T) {
//		sig := sk1.SignByte([]byte{1, 2, 3, 4})
//		sig.Add(sk2.SignByte([]byte{1, 2, 3, 4}))
//		msg1 := &PartialSignatureMessage{
//			Signers:         []types.NodeID{1, 2},
//			SigningRoot: []byte{1, 2, 3, 4},
//		}
//		msg2 := &PartialSignatureMessage{
//			Signers:         []types.NodeID{2},
//			SigningRoot: []byte{1, 2, 3, 4},
//		}
//
//		require.EqualError(t, msg1.Aggregate(msg2), "signer IDs partially/ fully match")
//	})
//
//	t.Run("different roots", func(t *testing.T) {
//		msg1 := &PartialSignatureMessage{
//			SigningRoot: []byte{1, 2, 3, 4},
//		}
//		msg2 := &PartialSignatureMessage{
//			SigningRoot: []byte{1, 2, 3, 3},
//		}
//
//		require.EqualError(t, msg1.Aggregate(msg2), "can't aggregate msgs with different roots")
//	})
//}

func TestSignedPartialSignatureMessage_Marshaling(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		signed := &ssv.SignedPartialSignatureMessage{
			Messages: ssv.PartialSignatureMessages{
				&ssv.PartialSignatureMessage{
					Slot:             1,
					PartialSignature: []byte{1, 2, 3, 4},
					SigningRoot:      []byte{1, 1, 1, 1},
					Signers:          []types.OperatorID{1},
				},
			},
			Signers:   []types.OperatorID{1},
			Signature: []byte{1, 2, 3, 4},
		}

		byts, err := signed.Encode()
		require.NoError(t, err)

		decoded := &ssv.SignedPartialSignatureMessage{}
		require.NoError(t, decoded.Decode(byts))
	})
}

func TestPartialSignatureMessage_Validate(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		m := &ssv.PartialSignatureMessage{
			PartialSignature: make([]byte, 96),
			SigningRoot:      make([]byte, 32),
			Signers:          []types.OperatorID{1},
		}
		require.NoError(t, m.Validate())
	})

	t.Run("invalid sig", func(t *testing.T) {
		m := &ssv.PartialSignatureMessage{
			PartialSignature: make([]byte, 95),
			SigningRoot:      make([]byte, 32),
			Signers:          []types.OperatorID{1},
		}
		require.EqualError(t, m.Validate(), "PartialSignatureMessage sig invalid")

		m.PartialSignature = make([]byte, 97)
		require.EqualError(t, m.Validate(), "PartialSignatureMessage sig invalid")

		m.PartialSignature = nil
		require.EqualError(t, m.Validate(), "PartialSignatureMessage sig invalid")
	})

	t.Run("invalid root", func(t *testing.T) {
		m := &ssv.PartialSignatureMessage{
			PartialSignature: make([]byte, 96),
			SigningRoot:      make([]byte, 31),
			Signers:          []types.OperatorID{1},
		}
		require.EqualError(t, m.Validate(), "SigningRoot invalid")

		m.SigningRoot = make([]byte, 33)
		require.EqualError(t, m.Validate(), "SigningRoot invalid")

		m.SigningRoot = nil
		require.EqualError(t, m.Validate(), "SigningRoot invalid")
	})

	t.Run("invalid signers", func(t *testing.T) {
		m := &ssv.PartialSignatureMessage{
			PartialSignature: make([]byte, 96),
			SigningRoot:      make([]byte, 32),
			Signers:          []types.OperatorID{},
		}
		require.EqualError(t, m.Validate(), "invalid PartialSignatureMessage signers")

		m.Signers = []types.OperatorID{1, 2}
		require.EqualError(t, m.Validate(), "invalid PartialSignatureMessage signers")

		m.Signers = nil
		require.EqualError(t, m.Validate(), "invalid PartialSignatureMessage signers")
	})
}

func TestSignedPartialSignatureMessage_Validate(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		m := &ssv.SignedPartialSignatureMessage{
			Signature: make([]byte, 96),
			Signers:   []types.OperatorID{1},
			Messages: ssv.PartialSignatureMessages{
				&ssv.PartialSignatureMessage{
					PartialSignature: make([]byte, 96),
					SigningRoot:      make([]byte, 32),
					Signers:          []types.OperatorID{1},
				},
			},
		}
		require.NoError(t, m.Validate())
	})

	t.Run("invalid sig", func(t *testing.T) {
		m := &ssv.SignedPartialSignatureMessage{
			Signature: make([]byte, 95),
			Signers:   []types.OperatorID{1},
			Messages: ssv.PartialSignatureMessages{
				&ssv.PartialSignatureMessage{
					PartialSignature: make([]byte, 96),
					SigningRoot:      make([]byte, 32),
					Signers:          []types.OperatorID{1},
				},
			},
		}
		require.EqualError(t, m.Validate(), "SignedPartialSignatureMessage sig invalid")

		m.Signature = make([]byte, 97)
		require.EqualError(t, m.Validate(), "SignedPartialSignatureMessage sig invalid")

		m.Signature = nil
		require.EqualError(t, m.Validate(), "SignedPartialSignatureMessage sig invalid")
	})

	t.Run("invalid signers", func(t *testing.T) {
		m := &ssv.SignedPartialSignatureMessage{
			Signature: make([]byte, 95),
			Signers:   []types.OperatorID{},
			Messages: ssv.PartialSignatureMessages{
				&ssv.PartialSignatureMessage{
					PartialSignature: make([]byte, 96),
					SigningRoot:      make([]byte, 32),
					Signers:          []types.OperatorID{1},
				},
			},
		}
		require.EqualError(t, m.Validate(), "SignedPartialSignatureMessage sig invalid")

		m.Signers = []types.OperatorID{1, 2}
		require.EqualError(t, m.Validate(), "SignedPartialSignatureMessage sig invalid")

		m.Signers = nil
		require.EqualError(t, m.Validate(), "SignedPartialSignatureMessage sig invalid")
	})

	t.Run("invalid msg", func(t *testing.T) {
		m := &ssv.SignedPartialSignatureMessage{
			Signature: make([]byte, 96),
			Signers:   []types.OperatorID{1},
			Messages: ssv.PartialSignatureMessages{
				&ssv.PartialSignatureMessage{
					PartialSignature: make([]byte, 95),
					SigningRoot:      make([]byte, 32),
					Signers:          []types.OperatorID{1},
				},
			},
		}
		require.EqualError(t, m.Validate(), "message invalid: PartialSignatureMessage sig invalid")
	})
}
