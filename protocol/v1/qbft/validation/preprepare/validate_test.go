package preprepare

import (
	"testing"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
)

// GenerateNodes generates randomly nodes
func GenerateNodes(cnt int) (map[uint64]*bls.SecretKey, map[uint64]*beacon.Node) {
	_ = bls.Init(bls.BLS12_381)
	nodes := make(map[uint64]*beacon.Node)
	sks := make(map[uint64]*bls.SecretKey)
	for i := 0; i < cnt; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()

		nodes[uint64(i)] = &beacon.Node{
			IbftID: uint64(i),
			Pk:     sk.GetPublicKey().Serialize(),
		}
		sks[uint64(i)] = sk
	}
	return sks, nodes
}

func signMessage(msg *specqbft.Message, sk *bls.SecretKey) (*bls.Sign, error) {
	signatureDomain := spectypes.ComputeSignatureDomain(spectypes.PrimusTestnet, spectypes.QBFTSignatureType)
	root, err := spectypes.ComputeSigningRoot(msg, signatureDomain)
	if err != nil {
		return nil, err
	}
	return sk.SignByte(root), nil
}

// SignMsg signs the given message by the given private key
func SignMsg(t *testing.T, id uint64, sk *bls.SecretKey, msg *specqbft.Message) *specqbft.SignedMessage {
	bls.Init(bls.BLS12_381)

	signature, err := signMessage(msg, sk)
	require.NoError(t, err)
	sm := &specqbft.SignedMessage{
		Message:   msg,
		Signature: signature.Serialize(),
		Signers:   []spectypes.OperatorID{spectypes.OperatorID(id)},
	}
	return sm
}

func TestValidatePrePrepareValue(t *testing.T) {
	sks, _ := GenerateNodes(4)

	tests := []struct {
		name string
		err  string
		msg  *specqbft.SignedMessage
	}{
		{
			"no signers",
			"proposal msg allows 1 signer",
			&specqbft.SignedMessage{
				Message: &specqbft.Message{
					MsgType:    specqbft.ProposalMsgType,
					Round:      1,
					Identifier: []byte("Lambda"),
					Data:       []byte(time.Now().Weekday().String()),
				},
				Signature: []byte{},
				Signers:   []spectypes.OperatorID{},
			},
		},
		{
			"only 2 signers",
			"proposal msg allows 1 signer",
			&specqbft.SignedMessage{
				Message: &specqbft.Message{
					MsgType:    specqbft.ProposalMsgType,
					Round:      1,
					Identifier: []byte("Lambda"),
					Data:       []byte(time.Now().Weekday().String()),
				},
				Signature: []byte{},
				Signers:   []spectypes.OperatorID{1, 2},
			},
		},
		{
			"non-leader sender",
			"proposal leader invalid",
			SignMsg(t, 2, sks[2], &specqbft.Message{
				MsgType:    specqbft.ProposalMsgType,
				Round:      1,
				Identifier: []byte("Lambda"),
				Data:       []byte("wrong value"),
			}),
		},
		{
			"valid message",
			"",
			SignMsg(t, 1, sks[1], &specqbft.Message{
				MsgType:    specqbft.ProposalMsgType,
				Round:      1,
				Identifier: []byte("Lambda"),
				Data:       []byte(time.Now().Weekday().String()),
			}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidatePrePrepareMsg(func(round specqbft.Round) uint64 {
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
