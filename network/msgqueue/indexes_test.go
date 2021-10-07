package msgqueue

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSigRoundIndexKey(t *testing.T) {
	require.EqualValues(t, "sig_lambda_01020304_seqNumber_2", SigRoundIndexKey([]byte{1, 2, 3, 4}, 2))
}

func TestSigMessageIndex(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		require.EqualValues(t, []string{"sig_lambda_01020304_seqNumber_2"}, sigMessageIndex()(&network.Message{
			SignedMessage: &proto.SignedMessage{
				Message: &proto.Message{
					Lambda:    []byte{1, 2, 3, 4},
					SeqNumber: 2,
				},
			},
			Type: network.NetworkMsg_SignatureType,
		}))
	})

	t.Run("invalid - no lambda", func(t *testing.T) {
		require.EqualValues(t, []string{}, sigMessageIndex()(&network.Message{
			SignedMessage: &proto.SignedMessage{
				Message: &proto.Message{
					SeqNumber: 2,
				},
			},
			Type: network.NetworkMsg_SignatureType,
		}))
	})

	t.Run("invalid - no message", func(t *testing.T) {
		require.EqualValues(t, []string{}, sigMessageIndex()(&network.Message{
			SignedMessage: &proto.SignedMessage{},
			Type:          network.NetworkMsg_SignatureType,
		}))
	})

	t.Run("invalid - no signed msg", func(t *testing.T) {
		require.EqualValues(t, []string{}, sigMessageIndex()(&network.Message{
			Type: network.NetworkMsg_SignatureType,
		}))
	})

	t.Run("invalid - wrong type", func(t *testing.T) {
		require.EqualValues(t, []string{}, sigMessageIndex()(&network.Message{
			SignedMessage: &proto.SignedMessage{
				Message: &proto.Message{
					Lambda:    []byte{1, 2, 3, 4},
					SeqNumber: 2,
				},
			},
			Type: network.NetworkMsg_IBFTType,
		}))
	})
}

func TestSyncIndexKey(t *testing.T) {
	require.EqualValues(t, "sync_lambda_01020304", SyncIndexKey([]byte{1, 2, 3, 4}))
}

func TestSyncMessageIndex(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		require.EqualValues(t, []string{"sync_lambda_01020304"}, syncMessageIndex()(&network.Message{
			SyncMessage: &network.SyncMessage{
				Lambda: []byte{1, 2, 3, 4},
			},
			Type: network.NetworkMsg_SyncType,
		}))
	})

	t.Run("invalid - no lambda", func(t *testing.T) {
		require.EqualValues(t, []string{}, syncMessageIndex()(&network.Message{
			SyncMessage: &network.SyncMessage{},
			Type:        network.NetworkMsg_SyncType,
		}))
	})

	t.Run("invalid - no sync message", func(t *testing.T) {
		require.EqualValues(t, []string{}, syncMessageIndex()(&network.Message{
			SignedMessage: &proto.SignedMessage{},
			Type:          network.NetworkMsg_SyncType,
		}))
	})

	t.Run("invalid - no sync message", func(t *testing.T) {
		require.EqualValues(t, []string{}, syncMessageIndex()(&network.Message{
			Type: network.NetworkMsg_SyncType,
		}))
	})

	t.Run("invalid - wrong type", func(t *testing.T) {
		require.EqualValues(t, []string{}, syncMessageIndex()(&network.Message{
			SignedMessage: &proto.SignedMessage{
				Message: &proto.Message{
					Lambda:    []byte{1, 2, 3, 4},
					SeqNumber: 1,
					Round:     2,
				},
			},
			Type: network.NetworkMsg_SignatureType,
		}))
	})
}

func TestIBFTMessageIndexKey(t *testing.T) {
	require.EqualValues(t, "lambda_01020304_seqNumber_1", IBFTMessageIndexKey([]byte{1, 2, 3, 4}, 1))
}

func TestIBFTMessageIndex(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		require.EqualValues(t, []string{"lambda_01020304_seqNumber_1"}, iBFTMessageIndex()(&network.Message{
			SignedMessage: &proto.SignedMessage{
				Message: &proto.Message{
					Lambda:    []byte{1, 2, 3, 4},
					SeqNumber: 1,
					Round:     2,
				},
			},
			Type: network.NetworkMsg_IBFTType,
		}))
	})

	t.Run("invalid - no lambda", func(t *testing.T) {
		require.EqualValues(t, []string{}, iBFTMessageIndex()(&network.Message{
			SignedMessage: &proto.SignedMessage{
				Message: &proto.Message{
					SeqNumber: 2,
				},
			},
			Type: network.NetworkMsg_IBFTType,
		}))
	})

	t.Run("invalid - no message", func(t *testing.T) {
		require.EqualValues(t, []string{}, iBFTMessageIndex()(&network.Message{
			SignedMessage: &proto.SignedMessage{},
			Type:          network.NetworkMsg_IBFTType,
		}))
	})

	t.Run("invalid - no signed msg", func(t *testing.T) {
		require.EqualValues(t, []string{}, iBFTMessageIndex()(&network.Message{
			Type: network.NetworkMsg_IBFTType,
		}))
	})

	t.Run("invalid - wrong type", func(t *testing.T) {
		require.EqualValues(t, []string{}, iBFTMessageIndex()(&network.Message{
			SignedMessage: &proto.SignedMessage{
				Message: &proto.Message{
					Lambda:    []byte{1, 2, 3, 4},
					SeqNumber: 1,
					Round:     2,
				},
			},
			Type: network.NetworkMsg_SignatureType,
		}))
	})
}

func TestDecidedIndexKey(t *testing.T) {
	require.EqualValues(t, "decided_lambda_01020304", DecidedIndexKey([]byte{1, 2, 3, 4}))
}

func TestDecidedMessageIndex(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		require.EqualValues(t, []string{"decided_lambda_01020304"}, decidedMessageIndex()(&network.Message{
			SignedMessage: &proto.SignedMessage{
				Message: &proto.Message{
					Lambda: []byte{1, 2, 3, 4},
				},
			},
			Type: network.NetworkMsg_DecidedType,
		}))
	})

	t.Run("invalid - no lambda", func(t *testing.T) {
		require.EqualValues(t, []string{}, decidedMessageIndex()(&network.Message{
			SignedMessage: &proto.SignedMessage{
				Message: &proto.Message{
					SeqNumber: 2,
				},
			},
			Type: network.NetworkMsg_DecidedType,
		}))
	})

	t.Run("invalid - no message", func(t *testing.T) {
		require.EqualValues(t, []string{}, decidedMessageIndex()(&network.Message{
			SignedMessage: &proto.SignedMessage{},
			Type:          network.NetworkMsg_DecidedType,
		}))
	})

	t.Run("invalid - no signed msg", func(t *testing.T) {
		require.EqualValues(t, []string{}, decidedMessageIndex()(&network.Message{
			Type: network.NetworkMsg_DecidedType,
		}))
	})

	t.Run("invalid - wrong type", func(t *testing.T) {
		require.EqualValues(t, []string{}, decidedMessageIndex()(&network.Message{
			SignedMessage: &proto.SignedMessage{
				Message: &proto.Message{
					Lambda:    []byte{1, 2, 3, 4},
					SeqNumber: 1,
					Round:     2,
				},
			},
			Type: network.NetworkMsg_SignatureType,
		}))
	})

}
