package changeround

import (
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	protocoltesting "github.com/bloxapp/ssv/protocol/v1/testing"
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

func aggregateSign(t *testing.T, sks map[spectypes.OperatorID]*bls.SecretKey, msg *specqbft.Message) *bls.Sign {
	var aggregatedSig *bls.Sign
	for _, sk := range sks {
		sig, err := signMessage(msg, sk)
		require.NoError(t, err)
		if aggregatedSig == nil {
			aggregatedSig = sig
		} else {
			aggregatedSig.Add(sig)
		}
	}
	return aggregatedSig
}

// SignMsg signs the given message by the given private key
func SignMsg(t *testing.T, id spectypes.OperatorID, sk *bls.SecretKey, msg *specqbft.Message) *specqbft.SignedMessage {
	bls.Init(bls.BLS12_381)

	signature, err := signMessage(msg, sk)
	require.NoError(t, err)

	return &specqbft.SignedMessage{
		Message:   msg,
		Signature: signature.Serialize(),
		Signers:   []spectypes.OperatorID{id},
	}
}

func TestValidateChangeRound(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	share := &beacon.Share{
		Committee: nodes,
	}
	pip := Validate(share)

	validMessage := &specqbft.Message{
		MsgType:    specqbft.PrepareMsgType,
		Height:     12,
		Round:      2,
		Identifier: []byte("Lambda"),
		Data:       encodePrepareData(t, []byte("value")),
	}
	aggSig := aggregateSign(t, sks, validMessage)

	tests := []struct {
		name string
		err  string
		msg  *specqbft.SignedMessage
	}{
		{
			"valid nil prepared change round",
			"",
			SignMsg(t, 1, sks[1], &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Round:      3,
				Height:     12,
				Identifier: []byte("Lambda"),
				Data:       changeRoundDataToBytes(t, &specqbft.RoundChangeData{}),
			}),
		},
		{
			"valid prepared change round",
			"",
			SignMsg(t, 1, sks[1], &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Round:      3,
				Height:     12,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(t, &specqbft.RoundChangeData{
					PreparedValue: []byte("value"),
					PreparedRound: 2,
					RoundChangeJustification: []*specqbft.SignedMessage{
						protocoltesting.SignMsg(t, sks, []spectypes.OperatorID{0}, validMessage),
						protocoltesting.SignMsg(t, sks, []spectypes.OperatorID{1}, validMessage),
						protocoltesting.SignMsg(t, sks, []spectypes.OperatorID{2}, validMessage),
						protocoltesting.SignMsg(t, sks, []spectypes.OperatorID{3}, validMessage),
					},
				}),
			}),
		},
		{
			"invalid prepared change round",
			"round change justification invalid: invalid message signature: failed to verify signature",
			SignMsg(t, 1, sks[1], &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Round:      3,
				Height:     12,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(t, &specqbft.RoundChangeData{
					PreparedValue: []byte("value"),
					PreparedRound: 2,
					RoundChangeJustification: []*specqbft.SignedMessage{
						{
							Signature: aggSig.Serialize(),
							Signers:   []spectypes.OperatorID{1},
							Message:   validMessage,
						},
					},
				}),
			}),
		},
		{
			"wrong sequence prepared change round",
			"round change justification invalid: change round justification sequence is wrong",
			SignMsg(t, 1, sks[1], &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Round:      3,
				Height:     12,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(t, &specqbft.RoundChangeData{
					PreparedValue: []byte("value"),
					PreparedRound: 2,
					RoundChangeJustification: []*specqbft.SignedMessage{
						{
							Signature: aggSig.Serialize(),
							Signers:   []spectypes.OperatorID{0, 1, 2, 3},
							Message: &specqbft.Message{
								MsgType:    specqbft.PrepareMsgType,
								Height:     11,
								Round:      2,
								Identifier: []byte("Lambda"),
								Data:       encodePrepareData(t, []byte("value")),
							},
						},
					},
				}),
			}),
		},
		{
			"missing change-round-data",
			"change round justification msg is nil",
			SignMsg(t, 1, sks[1], &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Round:      1,
				Height:     12,
				Identifier: []byte("Lambda"),
				Data:       nil,
			}),
		},
		{
			"non-valid change-round-data json",
			"could not get roundChange data : could not decode round change data from message: invalid character 'o' in literal null (expecting 'u')",
			SignMsg(t, 1, sks[1], &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Round:      1,
				Height:     12,
				Identifier: []byte("Lambda"),
				Data:       []byte("non-valid"),
			}),
		},
		{
			"justification type is not prepared",
			"round change justification invalid: change round justification msg type not Prepare (0)",
			SignMsg(t, 1, sks[1], &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Round:      1,
				Height:     12,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(t, &specqbft.RoundChangeData{
					PreparedValue: []byte("value"),
					RoundChangeJustification: []*specqbft.SignedMessage{
						{
							Message: &specqbft.Message{
								MsgType: specqbft.ProposalMsgType,
							},
						},
					},
				}),
			}),
		},
		{
			"small justification round",
			"round change justification invalid: round is wrong",
			SignMsg(t, 1, sks[1], &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Round:      1,
				Height:     12,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(t, &specqbft.RoundChangeData{
					PreparedValue: []byte("value"),
					PreparedRound: 0,
					RoundChangeJustification: []*specqbft.SignedMessage{
						{
							Message: &specqbft.Message{
								MsgType: specqbft.PrepareMsgType,
								Height:  12,
								Round:   1,
							},
						},
					},
				}),
			}),
		},
		{
			"bad lambda",
			"round change justification invalid: change round justification msg Lambda not equal to msg Lambda not equal to instance lambda",
			SignMsg(t, 1, sks[1], &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Round:      3,
				Height:     12,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(t, &specqbft.RoundChangeData{
					PreparedValue: []byte("value"),
					PreparedRound: 2,
					RoundChangeJustification: []*specqbft.SignedMessage{
						{
							Message: &specqbft.Message{
								MsgType:    specqbft.PrepareMsgType,
								Height:     12,
								Round:      2,
								Identifier: []byte("xxx"),
							},
						},
					},
				}),
			}),
		},
		{
			"bad value",
			"round change justification invalid: prepare data != proposed data",
			SignMsg(t, 1, sks[1], &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Round:      3,
				Height:     12,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(t, &specqbft.RoundChangeData{
					PreparedValue: []byte("value"),
					PreparedRound: 2,
					RoundChangeJustification: []*specqbft.SignedMessage{
						{
							Message: &specqbft.Message{
								MsgType:    specqbft.PrepareMsgType,
								Height:     12,
								Round:      2,
								Identifier: []byte("Lambda"),
								Data:       encodePrepareData(t, []byte("xxx")),
							},
						},
					},
				}),
			}),
		},
		{
			"insufficient number of signers",
			"no justifications quorum",
			SignMsg(t, 1, sks[1], &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Round:      3,
				Height:     12,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(t, &specqbft.RoundChangeData{
					PreparedValue: []byte("value"),
					PreparedRound: 2,
					RoundChangeJustification: []*specqbft.SignedMessage{
						protocoltesting.SignMsg(t, sks, []spectypes.OperatorID{1}, validMessage),
						protocoltesting.SignMsg(t, sks, []spectypes.OperatorID{2}, validMessage),
					},
				}),
			}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := pip.Run(test.msg)
			if len(test.err) > 0 {
				require.EqualError(t, err, test.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func encodePrepareData(t *testing.T, data []byte) []byte {
	result, err := (&specqbft.PrepareData{Data: data}).Encode()
	require.NoError(t, err)

	return result
}

func changeRoundDataToBytes(t *testing.T, data *specqbft.RoundChangeData) []byte {
	encoded, err := data.Encode()
	require.NoError(t, err)

	return encoded
}
