package auth

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/ibft/proto"
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
				Type:        proto.RoundState_Decided,
				Round:       4,
				Lambda:      []byte{1, 2, 3, 4},
				SeqNumber:   1,
				Value:       []byte("hello"),
				ValidatorPk: _byteArray("86b78e9d24f3efacbb3ca5958b39cdcb9b3e97d241e91c903f71392e1e4f5d7706a6c8e731e76d4e0e2ac52ccd35fcb9"),
			},
			[]uint64{1},
			[]*bls.SecretKey{sks[1]},
			nil,
			"",
		},
		{
			"valid aggregate sig",
			&proto.Message{
				Type:        proto.RoundState_Decided,
				Round:       4,
				Lambda:      []byte{1, 2, 3, 4},
				SeqNumber:   1,
				Value:       []byte("hello"),
				ValidatorPk: _byteArray("86b78e9d24f3efacbb3ca5958b39cdcb9b3e97d241e91c903f71392e1e4f5d7706a6c8e731e76d4e0e2ac52ccd35fcb9"),
			},
			[]uint64{1, 2},
			[]*bls.SecretKey{sks[1], sks[2]},
			nil,
			"",
		},
		{
			"invalid sig",
			&proto.Message{
				Type:        proto.RoundState_Decided,
				Round:       4,
				Lambda:      []byte{1, 2, 3, 4},
				SeqNumber:   1,
				Value:       []byte("hello"),
				ValidatorPk: _byteArray("86b78e9d24f3efacbb3ca5958b39cdcb9b3e97d241e91c903f71392e1e4f5d7706a6c8e731e76d4e0e2ac52ccd35fcb9"),
			},
			[]uint64{1},
			[]*bls.SecretKey{sks[1]},
			_byteArray("83ffa7e8e65a99fdff0bff0384d4abeee3e79023faceb7973893c541bd6b67f068d10a9986c9dc55f58d421d5f78b83f144c3f191f51cb6d0d655fa87184693329ef885aea1e7070c5ce76500dc86ac16e322d4298386aa330b88d90c2c5121d"),
			"could not verify message signature",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			signed := SignMsg(t, test.signerIds, test.sks, test.msg)
			if test.sig != nil {
				signed.Signature = test.sig
			}

			pipeline := AuthorizeMsg(&proto.InstanceParams{
				ConsensusParams: proto.DefaultConsensusParams(),
				IbftCommittee:   committee,
			})

			if len(test.expectedError) == 0 {
				require.NoError(t, pipeline.Run(signed))
			} else {
				require.EqualError(t, pipeline.Run(signed), test.expectedError)
			}
		})
	}
}
