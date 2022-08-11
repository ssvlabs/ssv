package prepare

import (
	"testing"
	"time"

	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/types"

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

func TestValidatePrepareMsg(t *testing.T) {
	sks, _ := GenerateNodes(4)

	tests := []struct {
		name string
		err  string
		msg  *specqbft.SignedMessage
	}{
		{
			"no signers",
			"prepare msg allows 1 signer",
			&specqbft.SignedMessage{
				Message: &specqbft.Message{
					MsgType:    specqbft.PrepareMsgType,
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
			"prepare msg allows 1 signer",
			&specqbft.SignedMessage{
				Message: &specqbft.Message{
					MsgType:    specqbft.PrepareMsgType,
					Round:      1,
					Identifier: []byte("Lambda"),
					Data:       []byte(time.Now().Weekday().String()),
				},
				Signature: []byte{},
				Signers:   []spectypes.OperatorID{1, 2},
			},
		},
		{
			"valid message",
			"",
			SignMsg(t, 1, sks[1], &specqbft.Message{
				MsgType:    specqbft.PrepareMsgType,
				Round:      1,
				Identifier: []byte("Lambda"),
				Data:       []byte(time.Now().Weekday().String()),
			}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidatePrepareMsg().Run(test.msg)
			if len(test.err) > 0 {
				require.EqualError(t, err, test.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateProposal(t *testing.T) {
	data1, err := (&specqbft.PrepareData{Data: []byte("data1")}).Encode()
	require.NoError(t, err)

	data2, err := (&specqbft.PrepareData{Data: []byte("data2")}).Encode()
	require.NoError(t, err)

	tests := []struct {
		name                            string
		msg                             *specqbft.SignedMessage
		proposalAcceptedForCurrentRound *specqbft.SignedMessage
		err                             string
	}{
		{
			"proposal has same value",
			&specqbft.SignedMessage{
				Message: &specqbft.Message{
					MsgType:    specqbft.PrepareMsgType,
					Round:      1,
					Identifier: []byte("Lambda"),
					Data:       data1,
				},
				Signature: []byte{},
				Signers:   []spectypes.OperatorID{},
			},
			&specqbft.SignedMessage{
				Message: &specqbft.Message{
					MsgType:    specqbft.PrepareMsgType,
					Round:      1,
					Identifier: []byte("Lambda"),
					Data:       data1,
				},
				Signature: []byte{},
				Signers:   []spectypes.OperatorID{},
			},
			"",
		},
		{
			"proposal has different value",
			&specqbft.SignedMessage{
				Message: &specqbft.Message{
					MsgType:    specqbft.PrepareMsgType,
					Round:      1,
					Identifier: []byte("Lambda"),
					Data:       data1,
				},
				Signature: []byte{},
				Signers:   []spectypes.OperatorID{},
			},
			&specqbft.SignedMessage{
				Message: &specqbft.Message{
					MsgType:    specqbft.PrepareMsgType,
					Round:      1,
					Identifier: []byte("Lambda"),
					Data:       data2,
				},
				Signature: []byte{},
				Signers:   []spectypes.OperatorID{},
			},
			"message data is different from proposed data",
		},
		{
			"no proposal",
			&specqbft.SignedMessage{
				Message: &specqbft.Message{
					MsgType:    specqbft.PrepareMsgType,
					Round:      1,
					Identifier: []byte("Lambda"),
					Data:       data1,
				},
				Signature: []byte{},
				Signers:   []spectypes.OperatorID{},
			},
			nil,
			"did not receive proposal for this round",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			state := &qbft.State{}
			state.ProposalAcceptedForCurrentRound.Store(tc.proposalAcceptedForCurrentRound)

			err := ValidateProposal(state).Run(tc.msg)
			if len(tc.err) > 0 {
				require.EqualError(t, err, tc.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
