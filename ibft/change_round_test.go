package ibft

import (
	"encoding/json"
	"github.com/bloxapp/ssv/utils/threshold"
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"

	msgcontinmem "github.com/bloxapp/ssv/ibft/msgcont/inmem"
	"github.com/bloxapp/ssv/ibft/pipeline/changeround"
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

func TestRoundChangeInputValue(t *testing.T) {
	secretKey, nodes := GenerateNodes(4)
	instance := &Instance{
		PrepareMessages: msgcontinmem.New(3),
		Params: &proto.InstanceParams{
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
	require.NotNil(t, byts)
	noPrepareChangeRoundData := proto.ChangeRoundData{}
	require.NoError(t, json.Unmarshal(byts, &noPrepareChangeRoundData))
	require.Nil(t, noPrepareChangeRoundData.PreparedValue)
	require.EqualValues(t, uint64(0), noPrepareChangeRoundData.PreparedRound)
	require.Nil(t, noPrepareChangeRoundData.JustificationSig)
	require.Nil(t, noPrepareChangeRoundData.JustificationMsg)
	require.Len(t, noPrepareChangeRoundData.SignerIds, 0)

	// add votes
	instance.PrepareMessages.AddMessage(SignMsg(t, 1, secretKey[1], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  1,
		Lambda: []byte("Lambda"),
		Value:  []byte("value"),
	}))
	instance.PrepareMessages.AddMessage(SignMsg(t, 2, secretKey[2], &proto.Message{
		Type:   proto.RoundState_Prepare,
		Round:  1,
		Lambda: []byte("Lambda"),
		Value:  []byte("value"),
	}))

	// with some prepare votes but not enough
	byts, err = instance.roundChangeInputValue()
	require.NoError(t, err)
	require.NotNil(t, byts)
	noPrepareChangeRoundData = proto.ChangeRoundData{}
	require.NoError(t, json.Unmarshal(byts, &noPrepareChangeRoundData))
	require.Nil(t, noPrepareChangeRoundData.PreparedValue)
	require.EqualValues(t, uint64(0), noPrepareChangeRoundData.PreparedRound)
	require.Nil(t, noPrepareChangeRoundData.JustificationSig)
	require.Nil(t, noPrepareChangeRoundData.JustificationMsg)
	require.Len(t, noPrepareChangeRoundData.SignerIds, 0)

	// add more votes
	instance.PrepareMessages.AddMessage(SignMsg(t, 3, secretKey[3], &proto.Message{
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
}

func TestValidateChangeRoundMessage(t *testing.T) {
	secretKeys, nodes := GenerateNodes(4)
	instance := &Instance{
		Params: &proto.InstanceParams{
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
		signerID            uint64
		justificationSigIds []uint64
		expectedError       string
	}{
		{
			name:     "valid",
			signerID: 1,
			msg: &proto.Message{
				Type:   proto.RoundState_ChangeRound,
				Round:  1,
				Lambda: []byte("Lambda"),
				Value:  changeRoundDataToBytes(&proto.ChangeRoundData{}),
			},
			expectedError: "",
		},
		{
			name:     "valid",
			signerID: 1,
			msg: &proto.Message{
				Type:   proto.RoundState_ChangeRound,
				Round:  2,
				Lambda: []byte("Lambda"),
				Value:  changeRoundDataToBytes(&proto.ChangeRoundData{}),
			},
			expectedError: "",
		},
		{
			name:     "valid",
			signerID: 1,
			msg: &proto.Message{
				Type:   proto.RoundState_ChangeRound,
				Round:  3,
				Lambda: []byte("Lambda"),
				Value:  changeRoundDataToBytes(&proto.ChangeRoundData{}),
			},
			expectedError: "",
		},
		{
			name:     "valid",
			signerID: 1,
			msg: &proto.Message{
				Type:   proto.RoundState_ChangeRound,
				Round:  3,
				Lambda: []byte("lambdas"),
				Value:  changeRoundDataToBytes(&proto.ChangeRoundData{}),
			},
			expectedError: "",
		},
		{
			name:     "nil ChangeRoundData",
			signerID: 1,
			msg: &proto.Message{
				Type:   proto.RoundState_ChangeRound,
				Round:  3,
				Lambda: []byte("lambdas"),
				Value:  nil,
			},
			expectedError: "change round justification msg is nil",
		},
		{
			name:                "valid justification",
			signerID:            1,
			justificationSigIds: []uint64{1, 2, 3},
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
					SignerIds:        []uint64{1, 2, 3},
				}),
			},
			expectedError: "",
		},
		{
			name:                "invalid justification msg type",
			signerID:            1,
			justificationSigIds: []uint64{1, 2, 3},
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
					SignerIds:        []uint64{1, 2, 3},
				}),
			},
			expectedError: "change round justification msg type not Prepare",
		},
		{
			name:                "invalid justification round",
			signerID:            1,
			justificationSigIds: []uint64{1, 2, 3},
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
					SignerIds:        []uint64{1, 2, 3},
				}),
			},
			expectedError: "change round justification round lower or equal to message round",
		},
		{
			name:                "invalid prepared and justification round",
			signerID:            1,
			justificationSigIds: []uint64{1, 2, 3},
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
					SignerIds:        []uint64{1, 2, 3},
				}),
			},
			expectedError: "change round prepared round not equal to justification msg round",
		},
		{
			name:                "invalid justification instance",
			signerID:            1,
			justificationSigIds: []uint64{1, 2, 3},
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
					SignerIds:        []uint64{1, 2, 3},
				}),
			},
			expectedError: "change round justification msg Lambda not equal to msg Lambda not equal to instance lambda",
		},
		{
			name:                "invalid justification quorum",
			signerID:            1,
			justificationSigIds: []uint64{1, 2},
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
					SignerIds:        []uint64{1, 2},
				}),
			},
			expectedError: "change round justification does not constitute a quorum",
		},
		{
			name:                "valid justification",
			signerID:            1,
			justificationSigIds: []uint64{1, 2, 3},
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
					SignerIds:        []uint64{1, 2, 3},
				}),
			},
			expectedError: "change round prepared value not equal to justification msg value",
		},
		{
			name:                "invalid justification sig",
			signerID:            1,
			justificationSigIds: []uint64{1, 2},
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
					SignerIds:        []uint64{1, 2, 3},
				}),
			},
			expectedError: "change round justification signature doesn't verify",
		},
	}

	threshold.Init()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// sign if needed
			if test.msg.Value != nil {
				var signature *bls.Sign
				data := bytesToChangeRoundData(test.msg.Value)
				if len(test.justificationSigIds) > 0 {
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
			}

			signature, err := test.msg.Sign(secretKeys[test.signerID])
			require.NoError(t, err)

			err = changeround.Validate(instance.Params).Run(&proto.SignedMessage{
				Message:   test.msg,
				Signature: signature.Serialize(),
				SignerIds: []uint64{test.signerID},
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
		ChangeRoundMessages: msgcontinmem.New(3),
		Params: &proto.InstanceParams{
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
	res, err := instance.JustifyRoundChange(2)
	require.NoError(t, err)
	require.False(t, res)

	instance.ChangeRoundMessages.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_ChangeRound,
			Round:  2,
			Lambda: []byte("Lambda"),
			Value:  changeRoundDataToBytes(&proto.ChangeRoundData{}),
		},
		SignerIds: []uint64{1},
	})
	instance.ChangeRoundMessages.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_ChangeRound,
			Round:  2,
			Lambda: []byte("Lambda"),
			Value:  changeRoundDataToBytes(&proto.ChangeRoundData{}),
		},
		SignerIds: []uint64{2},
	})
	instance.ChangeRoundMessages.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_ChangeRound,
			Round:  2,
			Lambda: []byte("Lambda"),
			Value:  changeRoundDataToBytes(&proto.ChangeRoundData{}),
		},
		SignerIds: []uint64{3},
	})

	// test no previous prepared round with round change quorum (no justification)
	res, err = instance.JustifyRoundChange(2)
	require.NoError(t, err)
	require.True(t, res)

	instance.ChangeRoundMessages = msgcontinmem.New(3)
	instance.ChangeRoundMessages.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_ChangeRound,
			Round:  1,
			Lambda: []byte("Lambda"),
			Value:  inputValue,
		},
		SignerIds: []uint64{1},
	})
	instance.ChangeRoundMessages.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_ChangeRound,
			Round:  2,
			Lambda: []byte("Lambda"),
			Value:  inputValue,
		},
		SignerIds: []uint64{2},
	})
	instance.ChangeRoundMessages.AddMessage(&proto.SignedMessage{
		Message: &proto.Message{
			Type:   proto.RoundState_ChangeRound,
			Round:  2,
			Lambda: []byte("Lambda"),
			Value:  inputValue,
		},
		SignerIds: []uint64{3},
	})

	// test no previous prepared round with round change quorum (with justification)
	res, err = instance.JustifyRoundChange(2)
	require.False(t, res)
	require.NoError(t, err)

	instance.State.PreparedRound = 1
	instance.State.PreparedValue = []byte("hello")
}

func TestHighestPrepared(t *testing.T) {
	inputValue := []byte("input value")

	instance := &Instance{
		ChangeRoundMessages: msgcontinmem.New(3),
		Params: &proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee: map[uint64]*proto.Node{
				0: {IbftId: 0},
				1: {IbftId: 1},
				2: {IbftId: 2},
				3: {IbftId: 3},
			},
		},
	}
	instance.ChangeRoundMessages.AddMessage(&proto.SignedMessage{
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
	instance.ChangeRoundMessages.AddMessage(&proto.SignedMessage{
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
	allNonPrepared, res, err := highestPrepared(3, instance.ChangeRoundMessages)
	require.NoError(t, err)
	require.False(t, allNonPrepared)
	require.EqualValues(t, 2, res.PreparedRound)
	require.EqualValues(t, append(inputValue, []byte("highest")...), res.PreparedValue)

	// test 2 equals
	instance.ChangeRoundMessages.AddMessage(&proto.SignedMessage{
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
	allNonPrepared, res, err = highestPrepared(3, instance.ChangeRoundMessages)
	require.NoError(t, err)
	require.False(t, allNonPrepared)
	require.EqualValues(t, 2, res.PreparedRound)
	require.EqualValues(t, append(inputValue, []byte("highest")...), res.PreparedValue)
}

func TestChangeRoundPipeline(t *testing.T) {
	_, nodes := GenerateNodes(4)
	instance := &Instance{
		PrepareMessages: msgcontinmem.New(3),
		Params: &proto.InstanceParams{
			ConsensusParams: proto.DefaultConsensusParams(),
			IbftCommittee:   nodes,
		},
		State: &proto.State{
			Round: 1,
		},
	}
	pipeline := instance.changeRoundMsgPipeline()
	require.EqualValues(t, "combination of: type check, lambda, round, validator PK, sequence, authorize, validate msg, add change round msg, upon partial quorum, upon change round full quorum, ", pipeline.Name())
}
