package ibft

import (
	"encoding/json"
	"testing"

	"github.com/bloxapp/ssv/ibft/pipeline/changeround"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"

	msgcontinmem "github.com/bloxapp/ssv/ibft/msgcont/inmem"
	"github.com/bloxapp/ssv/ibft/proto"
)

func changeRoundDataToBytes(input *proto.ChangeRoundData) []byte {
	ret, _ := json.Marshal(input)
	return ret
}
func bytesToChangeRoundData(input []byte) *proto.ChangeRoundData {
	ret := &proto.ChangeRoundData{}
	json.Unmarshal(input, ret)
	return ret
}

func signMsg(id uint64, secretKey *bls.SecretKey, msg *proto.Message) *proto.SignedMessage {
	signature, _ := msg.Sign(secretKey)
	return &proto.SignedMessage{
		Message:   msg,
		Signature: signature.Serialize(),
		SignerIds: []uint64{id},
	}
}

func TestRoundChangeInputValue(t *testing.T) {
	secretKey, nodes := generateNodes(4)
	instance := &Instance{
		prepareMessages: msgcontinmem.New(),
		params: &proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee:   nodes,
		},
		State: &proto.State{
			Round:         1,
			PreparedRound: 0,
			PreparedValue: nil,
		},
	}

	// no prepared round
	byts, err := instance.roundChangeInputValue()
	require.NoError(t, err)
	require.Nil(t, byts)

	// add votes
	instance.prepareMessages.AddMessage(signMsg(1, secretKey[1], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  1,
		Lambda: []byte("Lambda"),
		Value:  []byte("value"),
	}))
	instance.prepareMessages.AddMessage(signMsg(2, secretKey[2], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  1,
		Lambda: []byte("Lambda"),
		Value:  []byte("value"),
	}))

	// with some prepare votes but not enough
	byts, err = instance.roundChangeInputValue()
	require.NoError(t, err)
	require.Nil(t, byts)

	// add more votes
	instance.prepareMessages.AddMessage(signMsg(3, secretKey[3], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  1,
		Lambda: []byte("Lambda"),
		Value:  []byte("value"),
	}))
	instance.State.PreparedRound = 1
	instance.State.PreparedValue = []byte("value")

	// with a prepared round
	byts, err = instance.roundChangeInputValue()
	require.NoError(t, err)
	require.NotNil(t, byts)
	data := bytesToChangeRoundData(byts)
	require.EqualValues(t, 1, data.PreparedRound)
	require.EqualValues(t, []byte("value"), data.PreparedValue)

	// with a different prepared value
	instance.State.PreparedValue = []byte("value2")
	byts, err = instance.roundChangeInputValue()
	require.EqualError(t, err, "prepared value/ round is set but no quorum of prepare messages found")

	// with different prepared round
	instance.State.PreparedRound = 2
	instance.State.PreparedValue = []byte("value")
	byts, err = instance.roundChangeInputValue()
	require.EqualError(t, err, "prepared value/ round is set but no quorum of prepare messages found")
}

func TestValidateChangeRoundMessage(t *testing.T) {
	secretKeys, nodes := generateNodes(4)
	instance := &Instance{
		params: &proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee:   nodes,
		},
		State: &proto.State{
			Round:         1,
			PreparedRound: 0,
			PreparedValue: nil,
		},
	}

	tests := []struct {
		name                string
		msg                 *proto.Message
		signerId            uint64
		justificationSigIds []uint64
		expectedError       string
	}{
		{
			name:     "valid",
			signerId: 1,
			msg: &proto.Message{
				Type:   proto.RoundState_ChangeRound,
				Round:  1,
				Lambda: []byte("Lambda"),
				Value:  nil,
			},
			expectedError: "",
		},
		{
			name:     "valid",
			signerId: 1,
			msg: &proto.Message{
				Type:   proto.RoundState_ChangeRound,
				Round:  2,
				Lambda: []byte("Lambda"),
				Value:  nil,
			},
			expectedError: "",
		},
		{
			name:     "valid",
			signerId: 1,
			msg: &proto.Message{
				Type:   proto.RoundState_ChangeRound,
				Round:  3,
				Lambda: []byte("Lambda"),
				Value:  nil,
			},
			expectedError: "",
		},
		{
			name:     "valid",
			signerId: 1,
			msg: &proto.Message{
				Type:   proto.RoundState_ChangeRound,
				Round:  3,
				Lambda: []byte("lambdas"),
				Value:  nil,
			},
			expectedError: "",
		},
		{
			name:                "valid justification",
			signerId:            1,
			justificationSigIds: []uint64{0, 1, 2},
			msg: &proto.Message{
				Type:   proto.RoundState_ChangeRound,
				Round:  3,
				Lambda: []byte("lambdas"),
				Value: changeRoundDataToBytes(&proto.ChangeRoundData{
					PreparedRound: 2,
					PreparedValue: []byte("value"),
					JustificationMsg: &proto.Message{
						Type:   proto.RoundState_Prepare,
						Round:  2,
						Lambda: []byte("lambdas"),
						Value:  []byte("value"),
					},
					JustificationSig: nil,
					SignerIds:        []uint64{0, 1, 2},
				}),
			},
			expectedError: "",
		},
		{
			name:                "invalid justification msg type",
			signerId:            1,
			justificationSigIds: []uint64{0, 1, 2},
			msg: &proto.Message{
				Type:   proto.RoundState_ChangeRound,
				Round:  3,
				Lambda: []byte("lambdas"),
				Value: changeRoundDataToBytes(&proto.ChangeRoundData{
					PreparedRound: 2,
					PreparedValue: []byte("value"),
					JustificationMsg: &proto.Message{
						Type:   proto.RoundState_PrePrepare,
						Round:  2,
						Lambda: []byte("lambdas"),
						Value:  []byte("value"),
					},
					JustificationSig: nil,
					SignerIds:        []uint64{0, 1, 2},
				}),
			},
			expectedError: "change round justification msg type not Prepare",
		},
		{
			name:                "invalid justification round",
			signerId:            1,
			justificationSigIds: []uint64{0, 1, 2},
			msg: &proto.Message{
				Type:   proto.RoundState_ChangeRound,
				Round:  3,
				Lambda: []byte("lambdas"),
				Value: changeRoundDataToBytes(&proto.ChangeRoundData{
					PreparedRound: 2,
					PreparedValue: []byte("value"),
					JustificationMsg: &proto.Message{
						Type:   proto.RoundState_Prepare,
						Round:  3,
						Lambda: []byte("lambdas"),
						Value:  []byte("value"),
					},
					JustificationSig: nil,
					SignerIds:        []uint64{0, 1, 2},
				}),
			},
			expectedError: "change round justification round lower or equal to message round",
		},
		{
			name:                "invalid prepared and justification round",
			signerId:            1,
			justificationSigIds: []uint64{0, 1, 2},
			msg: &proto.Message{
				Type:   proto.RoundState_ChangeRound,
				Round:  3,
				Lambda: []byte("lambdas"),
				Value: changeRoundDataToBytes(&proto.ChangeRoundData{
					PreparedRound: 2,
					PreparedValue: []byte("value"),
					JustificationMsg: &proto.Message{
						Type:   proto.RoundState_Prepare,
						Round:  1,
						Lambda: []byte("lambdas"),
						Value:  []byte("value"),
					},
					JustificationSig: nil,
					SignerIds:        []uint64{0, 1, 2},
				}),
			},
			expectedError: "change round prepared round not equal to justification msg round",
		},
		{
			name:                "invalid justification instance",
			signerId:            1,
			justificationSigIds: []uint64{0, 1, 2},
			msg: &proto.Message{
				Type:   proto.RoundState_ChangeRound,
				Round:  3,
				Lambda: []byte("lambdas"),
				Value: changeRoundDataToBytes(&proto.ChangeRoundData{
					PreparedRound: 2,
					PreparedValue: []byte("value"),
					JustificationMsg: &proto.Message{
						Type:   proto.RoundState_Prepare,
						Round:  2,
						Lambda: []byte("Lambda"),
						Value:  []byte("value"),
					},
					JustificationSig: nil,
					SignerIds:        []uint64{0, 1, 2},
				}),
			},
			expectedError: "change round justification msg Lambda not equal to msg Lambda",
		},
		{
			name:                "valid justification",
			signerId:            1,
			justificationSigIds: []uint64{0, 1, 2},
			msg: &proto.Message{
				Type:   proto.RoundState_ChangeRound,
				Round:  3,
				Lambda: []byte("lambdas"),
				Value: changeRoundDataToBytes(&proto.ChangeRoundData{
					PreparedRound: 2,
					PreparedValue: []byte("value"),
					JustificationMsg: &proto.Message{
						Type:   proto.RoundState_Prepare,
						Round:  2,
						Lambda: []byte("lambdas"),
						Value:  []byte("values"),
					},
					JustificationSig: nil,
					SignerIds:        []uint64{0, 1, 2},
				}),
			},
			expectedError: "change round prepared value not equal to justification msg value",
		},
		{
			name:                "invalid justification sig",
			signerId:            1,
			justificationSigIds: []uint64{0, 1},
			msg: &proto.Message{
				Type:   proto.RoundState_ChangeRound,
				Round:  3,
				Lambda: []byte("lambdas"),
				Value: changeRoundDataToBytes(&proto.ChangeRoundData{
					PreparedRound: 2,
					PreparedValue: []byte("value"),
					JustificationMsg: &proto.Message{
						Type:   proto.RoundState_Prepare,
						Round:  2,
						Lambda: []byte("lambdas"),
						Value:  []byte("value"),
					},
					JustificationSig: nil,
					SignerIds:        []uint64{0, 1, 2},
				}),
			},
			expectedError: "change round justification signature doesn't verify",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// sign if needed
			if test.msg.Value != nil {
				var signature *bls.Sign
				data := bytesToChangeRoundData(test.msg.Value)
				for _, id := range test.justificationSigIds {
					sign, err := data.JustificationMsg.Sign(secretKeys[id])
					require.NoError(t, err)
					if signature == nil {
						signature = sign
					} else {
						signature.Add(sign)
					}
				}
				data.JustificationSig = signature.Serialize()
				test.msg.Value = changeRoundDataToBytes(data)
			}

			signature, err := test.msg.Sign(secretKeys[test.signerId])
			require.NoError(t, err)

			err = changeround.Validate(instance.params).Run(&proto.SignedMessage{
				Message:   test.msg,
				Signature: signature.Serialize(),
				SignerIds: []uint64{test.signerId},
			})
			if len(test.expectedError) > 0 {
				require.EqualError(t, err, test.expectedError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRoundChangeJustification(t *testing.T) {
	inputValue := changeRoundDataToBytes(&proto.ChangeRoundData{
		PreparedRound: 1,
		PreparedValue: []byte("hello"),
	})

	instance := &Instance{
		changeRoundMessages: msgcontinmem.New(),
		params: &proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee: map[uint64]*proto.Node{
				0: {IbftId: 0},
				1: {IbftId: 1},
				2: {IbftId: 2},
				3: {IbftId: 3},
			},
		},
		State: &proto.State{
			Round:         1,
			PreparedRound: 0,
			PreparedValue: nil,
		},
	}

	// test no previous prepared round and no round change quorum
	//res, err := instance.justifyRoundChange(2)
	//require.EqualError(t, err, "could not justify round change, did not find highest prepared")
	//require.False(t, res)

	instance.changeRoundMessages.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_ChangeRound,
			Round:  2,
			Lambda: []byte("Lambda"),
			Value:  nil,
		},
		SignerIds: []uint64{1},
	})
	instance.changeRoundMessages.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_ChangeRound,
			Round:  2,
			Lambda: []byte("Lambda"),
			Value:  nil,
		},
		SignerIds: []uint64{2},
	})
	instance.changeRoundMessages.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_ChangeRound,
			Round:  2,
			Lambda: []byte("Lambda"),
			Value:  nil,
		},
		SignerIds: []uint64{3},
	})

	// test no previous prepared round with round change quorum (no justification)
	res, err := instance.justifyRoundChange(2)
	require.NoError(t, err)
	require.True(t, res)

	instance.changeRoundMessages = msgcontinmem.New()
	instance.changeRoundMessages.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_ChangeRound,
			Round:  1,
			Lambda: []byte("Lambda"),
			Value:  inputValue,
		},
		SignerIds: []uint64{1},
	})
	instance.changeRoundMessages.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_ChangeRound,
			Round:  2,
			Lambda: []byte("Lambda"),
			Value:  inputValue,
		},
		SignerIds: []uint64{2},
	})
	instance.changeRoundMessages.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_ChangeRound,
			Round:  2,
			Lambda: []byte("Lambda"),
			Value:  inputValue,
		},
		SignerIds: []uint64{3},
	})

	// test no previous prepared round with round change quorum (with justification)
	res, err = instance.justifyRoundChange(2)
	require.Errorf(t, err, "could not justify round change, did not receive quorum of prepare messages previously")
	require.False(t, res)

	instance.State.PreparedRound = 1
	instance.State.PreparedValue = []byte("hello")

	// test previously prepared round with round change quorum (with justification)
	res, err = instance.justifyRoundChange(2)
	require.NoError(t, err)
	require.True(t, res)
}

func TestHighestPrepared(t *testing.T) {
	inputValue := []byte("input value")

	instance := &Instance{
		changeRoundMessages: msgcontinmem.New(),
		params: &proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee: map[uint64]*proto.Node{
				0: {IbftId: 0},
				1: {IbftId: 1},
				2: {IbftId: 2},
				3: {IbftId: 3},
			},
		},
	}
	instance.changeRoundMessages.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_ChangeRound,
			Round:  3,
			Lambda: []byte("Lambda"),
			Value: changeRoundDataToBytes(&proto.ChangeRoundData{
				PreparedRound: 1,
				PreparedValue: inputValue,
			}),
		},
		SignerIds: []uint64{1},
	})
	instance.changeRoundMessages.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_ChangeRound,
			Round:  3,
			Lambda: []byte("Lambda"),
			Value: changeRoundDataToBytes(&proto.ChangeRoundData{
				PreparedRound: 2,
				PreparedValue: append(inputValue, []byte("highest")...),
			}),
		},
		SignerIds: []uint64{2},
	})

	// test one higher than other
	res, err := instance.highestPrepared(3)
	require.NoError(t, err)
	require.EqualValues(t, 2, res.PreparedRound)
	require.EqualValues(t, append(inputValue, []byte("highest")...), res.PreparedValue)

	// test 2 equals
	instance.changeRoundMessages.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_ChangeRound,
			Round:  3,
			Lambda: []byte("Lambda"),
			Value: changeRoundDataToBytes(&proto.ChangeRoundData{
				PreparedRound: 2,
				PreparedValue: append(inputValue, []byte("highest")...),
			}),
		},
		SignerIds: []uint64{2},
	})
	res, err = instance.highestPrepared(3)
	require.NoError(t, err)
	require.EqualValues(t, 2, res.PreparedRound)
	require.EqualValues(t, append(inputValue, []byte("highest")...), res.PreparedValue)
}
