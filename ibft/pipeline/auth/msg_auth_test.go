package auth

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/utils/threshold"
	"github.com/bloxapp/ssv/validator/storage"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"testing"
)

func _byteArray(input string) []byte {
	res, _ := hex.DecodeString(input)
	return res
}

// GenerateNodes generates randomly nodes
func GenerateNodes(cnt int) (map[uint64]*bls.SecretKey, map[uint64]*proto.Node) {
	_ = bls.Init(bls.BLS12_381)
	nodes := make(map[uint64]*proto.Node)
	sks := make(map[uint64]*bls.SecretKey)
	for i := 1; i <= cnt; i++ {
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
func SignMsg(t *testing.T, ids []uint64, sks []*bls.SecretKey, msg *proto.Message) *proto.SignedMessage {
	bls.Init(bls.BLS12_381)

	var agg *bls.Sign
	for _, sk := range sks {
		signature, err := msg.Sign(sk)
		require.NoError(t, err)
		if agg == nil {
			agg = signature
		} else {
			agg.Add(signature)
		}
	}

	require.NotNil(t, agg)
	return &proto.SignedMessage{
		Message:   msg,
		Signature: agg.Serialize(),
		SignerIds: ids,
	}
}

func TestAuthorizeMsg(t *testing.T) {
	threshold.Init()
	sks, committee := GenerateNodes(4)
	tests := []struct {
		name          string
		msg           *proto.Message
		signerIds     []uint64
		sks           []*bls.SecretKey
		sig           []byte
		expectedError string
	}{
		{
			"valid sig",
			&proto.Message{
				Type:      proto.RoundState_Decided,
				Round:     4,
				Lambda:    []byte{1, 2, 3, 4},
				SeqNumber: 1,
				Value:     []byte("hello"),
			},
			[]uint64{1},
			[]*bls.SecretKey{sks[1]},
			nil,
			"",
		},
		{
			"valid aggregate sig",
			&proto.Message{
				Type:      proto.RoundState_Decided,
				Round:     4,
				Lambda:    []byte{1, 2, 3, 4},
				SeqNumber: 1,
				Value:     []byte("hello"),
			},
			[]uint64{1, 2},
			[]*bls.SecretKey{sks[1], sks[2]},
			nil,
			"",
		},
		{
			"invalid sig",
			&proto.Message{
				Type:      proto.RoundState_Decided,
				Round:     4,
				Lambda:    []byte{1, 2, 3, 4},
				SeqNumber: 1,
				Value:     []byte("hello"),
			},
			[]uint64{1},
			[]*bls.SecretKey{sks[1]},
			_byteArray("90d44ba2e926c07a71086d3edd04d433746a80335c828f415c0dcb505a1357a454e94338a2139b201d031e4aa6294f3110caa5f2f9ecdd3727fcc9b3ea733e1819993ba06d175cfc55525515d46ef035d1c8bf5c9dab7536b51d936708aeaa22"),
			"could not verify message signature",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			signed := SignMsg(t, test.signerIds, test.sks, test.msg)
			if test.sig != nil {
				signed.Signature = test.sig
			}

			pipeline := AuthorizeMsg(&storage.Share{
				Committee: committee,
			})

			if len(test.expectedError) == 0 {
				require.NoError(t, pipeline.Run(signed))
			} else {
				require.EqualError(t, pipeline.Run(signed), test.expectedError)
			}
		})
	}
}
