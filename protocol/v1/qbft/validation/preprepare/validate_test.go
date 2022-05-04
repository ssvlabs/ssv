package preprepare

import (
	"testing"
	"time"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/ibft/proto"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/message"
)

// GenerateNodes generates randomly nodes
func GenerateNodes(cnt int) (map[uint64]*bls.SecretKey, map[uint64]*proto.Node) {
	_ = bls.Init(bls.BLS12_381)
	nodes := make(map[uint64]*proto.Node)
	sks := make(map[uint64]*bls.SecretKey)
	for i := 0; i < cnt; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()

		nodes[uint64(i)] = &proto.Node{
			IbftId: uint64(i),
			Pk:     sk.GetPublicKey().Serialize(),
		}
		sks[uint64(i)] = sk
	}
	return sks, nodes
}

// SignMsg signs the given message by the given private key
func SignMsg(t *testing.T, id uint64, sk *bls.SecretKey, msg *message.ConsensusMessage) *message.SignedMessage {
	bls.Init(bls.BLS12_381)

	signature, err := msg.Sign(sk, string(forksprotocol.V1ForkVersion))
	require.NoError(t, err)
	sm := &message.SignedMessage{
		Message:   msg,
		Signature: signature.Serialize(),
		Signers:   []message.OperatorID{message.OperatorID(id)},
	}
	return sm
}

func TestValidatePrePrepareValue(t *testing.T) {
	sks, _ := GenerateNodes(4)

	tests := []struct {
		name string
		err  string
		msg  *message.SignedMessage
	}{
		{
			"no signers",
			"invalid number of signers for pre-prepare message",
			&message.SignedMessage{
				Message: &message.ConsensusMessage{
					MsgType:    message.ProposalMsgType,
					Round:      1,
					Identifier: []byte("Lambda"),
					Data:       []byte(time.Now().Weekday().String()),
				},
				Signature: []byte{},
				Signers:   []message.OperatorID{},
			},
		},
		{
			"only 2 signers",
			"invalid number of signers for pre-prepare message",
			&message.SignedMessage{
				Message: &message.ConsensusMessage{
					MsgType:    message.ProposalMsgType,
					Round:      1,
					Identifier: []byte("Lambda"),
					Data:       []byte(time.Now().Weekday().String()),
				},
				Signature: []byte{},
				Signers:   []message.OperatorID{1, 2},
			},
		},
		{
			"non-leader sender",
			"pre-prepare message sender (id 2) is not the round's leader (expected 1)",
			SignMsg(t, 2, sks[2], &message.ConsensusMessage{
				MsgType:    message.ProposalMsgType,
				Round:      1,
				Identifier: []byte("Lambda"),
				Data:       []byte("wrong value"),
			}),
		},
		{
			"valid message",
			"",
			SignMsg(t, 1, sks[1], &message.ConsensusMessage{
				MsgType:    message.ProposalMsgType,
				Round:      1,
				Identifier: []byte("Lambda"),
				Data:       []byte(time.Now().Weekday().String()),
			}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidatePrePrepareMsg(func(round message.Round) uint64 {
				return 1
			}).Run(test.msg)
			if len(test.err) > 0 {
				require.EqualError(t, err, test.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
