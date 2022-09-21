package proposal

import (
	"testing"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/types"
)

// GenerateNodes generates randomly nodes
func GenerateNodes(cnt int) (map[spectypes.OperatorID]*bls.SecretKey, map[spectypes.OperatorID]*beacon.Node) {
	_ = bls.Init(bls.BLS12_381)
	nodes := make(map[spectypes.OperatorID]*beacon.Node)
	sks := make(map[spectypes.OperatorID]*bls.SecretKey)
	for i := 0; i < cnt; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()

		nodes[spectypes.OperatorID(i)] = &beacon.Node{
			IbftID: uint64(i),
			Pk:     sk.GetPublicKey().Serialize(),
		}
		sks[spectypes.OperatorID(i)] = sk
	}
	return sks, nodes
}

func signMessage(msg *specqbft.Message, sk *bls.SecretKey) (*bls.Sign, error) {
	signatureDomain := spectypes.ComputeSignatureDomain(types.GetDefaultDomain(), spectypes.QBFTSignatureType)
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

func TestValidateProposalValue(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	share := &beacon.Share{
		Committee: nodes,
	}

	validPrepareMsg := &specqbft.PrepareData{Data: []byte(time.Now().Weekday().String())}
	validEncodedPrepare, err := validPrepareMsg.Encode()
	require.NoError(t, err)

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
					Identifier: []byte("Identifier"),
					Data:       validEncodedPrepare,
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
					Identifier: []byte("Identifier"),
					Data:       validEncodedPrepare,
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
				Identifier: []byte("Identifier"),
				Data:       []byte("wrong value"),
			}),
		},
		{
			"valid message",
			"",
			SignMsg(t, 1, sks[1], &specqbft.Message{
				MsgType:    specqbft.ProposalMsgType,
				Round:      1,
				Identifier: []byte("Identifier"),
				Data:       validEncodedPrepare,
			}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := &qbft.State{}
			state.Round.Store(test.msg.Message.Round)

			err := ValidateProposalMsg(share, state, func(round specqbft.Round) uint64 {
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
