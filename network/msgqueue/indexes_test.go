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
	require.EqualValues(t, []string{"sig_lambda_01020304_seqNumber_2"}, sigMessageIndex()(&network.Message{
		SignedMessage: &proto.SignedMessage{
			Message: &proto.Message{
				Lambda:    []byte{1, 2, 3, 4},
				SeqNumber: 2,
			},
		},
		Type: network.NetworkMsg_SignatureType,
	}))

	require.EqualValues(t, []string{}, sigMessageIndex()(&network.Message{
		SignedMessage: &proto.SignedMessage{
			Message: &proto.Message{
				SeqNumber: 2,
			},
		},
		Type: network.NetworkMsg_SignatureType,
	}))
	require.EqualValues(t, []string{}, sigMessageIndex()(&network.Message{
		SignedMessage: &proto.SignedMessage{},
		Type:          network.NetworkMsg_SignatureType,
	}))
	require.EqualValues(t, []string{}, sigMessageIndex()(&network.Message{
		Type: network.NetworkMsg_SignatureType,
	}))
	require.EqualValues(t, []string{}, sigMessageIndex()(&network.Message{
		SignedMessage: &proto.SignedMessage{
			Message: &proto.Message{
				Lambda:    []byte{1, 2, 3, 4},
				SeqNumber: 2,
			},
		},
		Type: network.NetworkMsg_IBFTType,
	}))
}

func TestSyncIndexKey(t *testing.T) {
	require.EqualValues(t, "sync_lambda_01020304", SyncIndexKey([]byte{1, 2, 3, 4}))
}

func TestSyncMessageIndex(t *testing.T) {
	require.EqualValues(t, []string{"sync_lambda_01020304"}, syncMessageIndex()(&network.Message{
		SyncMessage: &network.SyncMessage{
			Lambda: []byte{1, 2, 3, 4},
		},
		Type: network.NetworkMsg_SyncType,
	}))

	require.EqualValues(t, []string{}, syncMessageIndex()(&network.Message{
		SyncMessage: &network.SyncMessage{},
		Type:        network.NetworkMsg_SyncType,
	}))
	require.EqualValues(t, []string{}, syncMessageIndex()(&network.Message{
		SignedMessage: &proto.SignedMessage{},
		Type:          network.NetworkMsg_SyncType,
	}))
	require.EqualValues(t, []string{}, syncMessageIndex()(&network.Message{
		Type: network.NetworkMsg_SyncType,
	}))
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
}

func TestIBFTMessageIndexKey(t *testing.T) {
	require.EqualValues(t, "lambda_01020304_seqNumber_1_round_2", IBFTMessageIndexKey([]byte{1, 2, 3, 4}, 1, 2))
}

func TestIBFTMessageIndex(t *testing.T) {
	require.EqualValues(t, []string{"lambda_01020304_seqNumber_1_round_2"}, iBFTMessageIndex()(&network.Message{
		SignedMessage: &proto.SignedMessage{
			Message: &proto.Message{
				Lambda:    []byte{1, 2, 3, 4},
				SeqNumber: 1,
				Round:     2,
			},
		},
		Type: network.NetworkMsg_IBFTType,
	}))

	require.EqualValues(t, []string{}, iBFTMessageIndex()(&network.Message{
		SignedMessage: &proto.SignedMessage{
			Message: &proto.Message{
				SeqNumber: 2,
			},
		},
		Type: network.NetworkMsg_IBFTType,
	}))
	require.EqualValues(t, []string{}, iBFTMessageIndex()(&network.Message{
		SignedMessage: &proto.SignedMessage{},
		Type:          network.NetworkMsg_IBFTType,
	}))
	require.EqualValues(t, []string{}, iBFTMessageIndex()(&network.Message{
		Type: network.NetworkMsg_IBFTType,
	}))
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
}

func TestDecidedIndexKey(t *testing.T) {
	require.EqualValues(t, "decided_lambda_01020304", DecidedIndexKey([]byte{1, 2, 3, 4}))
}

func TestDecidedMessageIndex(t *testing.T) {
	require.EqualValues(t, []string{"decided_lambda_01020304"}, decidedMessageIndex()(&network.Message{
		SignedMessage: &proto.SignedMessage{
			Message: &proto.Message{
				Lambda: []byte{1, 2, 3, 4},
			},
		},
		Type: network.NetworkMsg_DecidedType,
	}))

	require.EqualValues(t, []string{}, decidedMessageIndex()(&network.Message{
		SignedMessage: &proto.SignedMessage{
			Message: &proto.Message{
				SeqNumber: 2,
			},
		},
		Type: network.NetworkMsg_DecidedType,
	}))
	require.EqualValues(t, []string{}, decidedMessageIndex()(&network.Message{
		SignedMessage: &proto.SignedMessage{},
		Type:          network.NetworkMsg_DecidedType,
	}))
	require.EqualValues(t, []string{}, decidedMessageIndex()(&network.Message{
		Type: network.NetworkMsg_DecidedType,
	}))
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
}
