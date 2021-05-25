package preprepare

import (
	"github.com/herumi/bls-eth-go-binary/bls"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/utils/dataval/bytesval"
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
func SignMsg(t *testing.T, id uint64, sk *bls.SecretKey, msg *proto.Message) *proto.SignedMessage {
	bls.Init(bls.BLS12_381)

	signature, err := msg.Sign(sk)
	require.NoError(t, err)
	return &proto.SignedMessage{
		Message:   msg,
		Signature: signature.Serialize(),
		SignerIds: []uint64{id},
	}
}

type testLeaderSelector struct {
}

func (s *testLeaderSelector) Current(committeeSize uint64) uint64 {
	return 1
}
func (s *testLeaderSelector) Bump()                                   {}
func (s *testLeaderSelector) SetSeed(seed []byte, index uint64) error { return nil }

func TestValidatePrePrepareValue(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	params := &proto.InstanceParams{
		ConsensusParams: proto.DefaultConsensusParams(),
		IbftCommittee:   nodes,
	}
	consensus := bytesval.New([]byte(time.Now().Weekday().String()))

	tests := []struct {
		name string
		err  string
		msg  *proto.SignedMessage
	}{
		{
			"no signers",
			"invalid number of signers for pre-prepare message",
			&proto.SignedMessage{
				Message: &proto.Message{
					Type:   proto.RoundState_PrePrepare,
					Round:  1,
					Lambda: []byte("Lambda"),
					Value:  []byte(time.Now().Weekday().String()),
				},
				Signature: []byte{},
				SignerIds: []uint64{},
			},
		},
		{
			"only 2 signers",
			"invalid number of signers for pre-prepare message",
			&proto.SignedMessage{
				Message: &proto.Message{
					Type:   proto.RoundState_PrePrepare,
					Round:  1,
					Lambda: []byte("Lambda"),
					Value:  []byte(time.Now().Weekday().String()),
				},
				Signature: []byte{},
				SignerIds: []uint64{1, 2},
			},
		},
		{
			"wrong message",
			"failed while validating pre-prepare: msg value is wrong",
			SignMsg(t, 1, sks[1], &proto.Message{
				Type:   proto.RoundState_PrePrepare,
				Round:  1,
				Lambda: []byte("Lambda"),
				Value:  []byte("wrong value"),
			}),
		},
		{
			"non-leader sender",
			"pre-prepare message sender is not the round's leader",
			SignMsg(t, 2, sks[2], &proto.Message{
				Type:   proto.RoundState_PrePrepare,
				Round:  1,
				Lambda: []byte("Lambda"),
				Value:  []byte("wrong value"),
			}),
		},
		{
			"valid message",
			"",
			SignMsg(t, 1, sks[1], &proto.Message{
				Type:   proto.RoundState_PrePrepare,
				Round:  1,
				Lambda: []byte("Lambda"),
				Value:  []byte(time.Now().Weekday().String()),
			}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidatePrePrepareMsg(consensus, &testLeaderSelector{}, params).Run(test.msg)
			if len(test.err) > 0 {
				require.EqualError(t, err, test.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
