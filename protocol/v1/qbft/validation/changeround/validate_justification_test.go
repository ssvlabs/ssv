package changeround

import (
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"

	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
)

// GenerateNodes generates randomly nodes
func GenerateNodes(cnt int) (map[message.OperatorID]*bls.SecretKey, map[message.OperatorID]*beacon.Node) {
	_ = bls.Init(bls.BLS12_381)
	nodes := make(map[message.OperatorID]*beacon.Node)
	sks := make(map[message.OperatorID]*bls.SecretKey)
	for i := 0; i < cnt; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()

		nodes[message.OperatorID(i)] = &beacon.Node{
			IbftID: uint64(i),
			Pk:     sk.GetPublicKey().Serialize(),
		}
		sks[message.OperatorID(i)] = sk
	}
	return sks, nodes
}

func aggregateSign(t *testing.T, sks map[message.OperatorID]*bls.SecretKey, msg *message.ConsensusMessage) *bls.Sign {
	var aggregatedSig *bls.Sign
	for _, sk := range sks {
		sig, err := msg.Sign(sk, forksprotocol.V1ForkVersion.String())
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
func SignMsg(t *testing.T, id message.OperatorID, sk *bls.SecretKey, msg *message.ConsensusMessage) *message.SignedMessage {
	bls.Init(bls.BLS12_381)

	signature, err := msg.Sign(sk, forksprotocol.V1ForkVersion.String())
	require.NoError(t, err)

	return &message.SignedMessage{
		Message:   msg,
		Signature: signature.Serialize(),
		Signers:   []message.OperatorID{id},
	}
}

func TestValidateChangeRound(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	share := &beacon.Share{
		Committee: nodes,
	}
	pip := Validate(share, forksprotocol.V1ForkVersion.String())

	validMessage := &message.ConsensusMessage{
		MsgType:    message.PrepareMsgType,
		Height:     12,
		Round:      2,
		Identifier: []byte("Lambda"),
		Data:       encodePrepareData(t, []byte("value")),
	}
	validSig := aggregateSign(t, sks, validMessage)

	tests := []struct {
		name string
		err  string
		msg  *message.SignedMessage
	}{
		{
			"valid nil prepared change round",
			"",
			SignMsg(t, 1, sks[1], &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Round:      3,
				Height:     12,
				Identifier: []byte("Lambda"),
				Data:       changeRoundDataToBytes(t, &message.RoundChangeData{}),
			}),
		},
		{
			"valid prepared change round",
			"",
			SignMsg(t, 1, sks[1], &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Round:      3,
				Height:     12,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(t, &message.RoundChangeData{
					PreparedValue: []byte("value"),
					Round:         2,
					RoundChangeJustification: []*message.SignedMessage{
						{
							Signature: validSig.Serialize(),
							Signers:   []message.OperatorID{0, 1, 2, 3},
							Message:   validMessage,
						},
					},
				}),
			}),
		},
		{
			"invalid prepared change round",
			"change round could not verify signature: failed to verify signature",
			SignMsg(t, 1, sks[1], &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Round:      3,
				Height:     12,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(t, &message.RoundChangeData{
					PreparedValue: []byte("value"),
					Round:         2,
					RoundChangeJustification: []*message.SignedMessage{
						{
							Signature: validSig.Serialize(),
							Signers:   []message.OperatorID{1, 2, 3},
							Message:   validMessage,
						},
					},
				}),
			}),
		},
		{
			"wrong sequence prepared change round",
			"change round justification sequence is wrong",
			SignMsg(t, 1, sks[1], &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Round:      3,
				Height:     12,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(t, &message.RoundChangeData{
					PreparedValue: []byte("value"),
					Round:         2,
					RoundChangeJustification: []*message.SignedMessage{
						{
							Signature: validSig.Serialize(),
							Signers:   []message.OperatorID{0, 1, 2, 3},
							Message: &message.ConsensusMessage{
								MsgType:    message.PrepareMsgType,
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
			"failed to get round change data: could not decode change round data from message: unexpected end of JSON input",
			SignMsg(t, 1, sks[1], &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Round:      1,
				Height:     12,
				Identifier: []byte("Lambda"),
				Data:       nil,
			}),
		},
		{
			"non-valid change-round-data json",
			"failed to get round change data: could not decode change round data from message: invalid character 'o' in literal null (expecting 'u')",
			SignMsg(t, 1, sks[1], &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Round:      1,
				Height:     12,
				Identifier: []byte("Lambda"),
				Data:       []byte("non-valid"),
			}),
		},
		{
			"justification type is not prepared",
			"change round justification msg type not Prepare (0)",
			SignMsg(t, 1, sks[1], &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Round:      1,
				Height:     12,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(t, &message.RoundChangeData{
					PreparedValue: []byte("value"),
					RoundChangeJustification: []*message.SignedMessage{
						{
							Message: &message.ConsensusMessage{
								MsgType: message.ProposalMsgType,
							},
						},
					},
				}),
			}),
		},
		{
			"small justification round",
			"change round justification round lower or equal to message round",
			SignMsg(t, 1, sks[1], &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Round:      1,
				Height:     12,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(t, &message.RoundChangeData{
					PreparedValue: []byte("value"),
					Round:         0,
					RoundChangeJustification: []*message.SignedMessage{
						{
							Message: &message.ConsensusMessage{
								MsgType: message.PrepareMsgType,
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
			"change round justification msg Lambda not equal to msg Lambda not equal to instance lambda",
			SignMsg(t, 1, sks[1], &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Round:      3,
				Height:     12,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(t, &message.RoundChangeData{
					PreparedValue: []byte("value"),
					Round:         2,
					RoundChangeJustification: []*message.SignedMessage{
						{
							Message: &message.ConsensusMessage{
								MsgType:    message.PrepareMsgType,
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
			"change round prepared value not equal to justification msg value",
			SignMsg(t, 1, sks[1], &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Round:      3,
				Height:     12,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(t, &message.RoundChangeData{
					PreparedValue: []byte("value"),
					Round:         2,
					RoundChangeJustification: []*message.SignedMessage{
						{
							Message: &message.ConsensusMessage{
								MsgType:    message.PrepareMsgType,
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
			"change round justification does not constitute a quorum",
			SignMsg(t, 1, sks[1], &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Round:      3,
				Height:     12,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(t, &message.RoundChangeData{
					PreparedValue: []byte("value"),
					Round:         2,
					RoundChangeJustification: []*message.SignedMessage{
						{
							Signers: []message.OperatorID{1, 2},
							Message: validMessage,
						},
					},
				}),
			}),
		},
		{
			"duplicated signers",
			"change round could not verify signature: failed to verify signature",
			SignMsg(t, 1, sks[1], &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Round:      3,
				Height:     12,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(t, &message.RoundChangeData{
					PreparedValue: []byte("value"),
					Round:         2,
					RoundChangeJustification: []*message.SignedMessage{
						{
							Signature: validSig.Serialize(),
							Signers:   []message.OperatorID{1, 2, 2},
							Message:   validMessage,
						},
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
	result, err := (&message.PrepareData{Data: data}).Encode()
	require.NoError(t, err)

	return result
}

func changeRoundDataToBytes(t *testing.T, data *message.RoundChangeData) []byte {
	encoded, err := data.Encode()
	require.NoError(t, err)

	return encoded
}
