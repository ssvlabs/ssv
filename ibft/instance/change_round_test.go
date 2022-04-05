package ibft

import (
	"encoding/json"
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/protocol/v1/keymanager"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"github.com/bloxapp/ssv/utils/threadsafe"
	"github.com/bloxapp/ssv/utils/threshold"
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"

	msgcontinmem "github.com/bloxapp/ssv/ibft/instance/msgcont/inmem"
	"github.com/bloxapp/ssv/ibft/pipeline/changeround"
	"github.com/bloxapp/ssv/ibft/proto"
)

type testFork struct {
	instance *Instance
}

func (v0 *testFork) Apply(instance instance.Instance) {

}

// PrePrepareMsgPipelineV0 is the full processing msg pipeline for a pre-prepare msg
func (v0 *testFork) PrePrepareMsgPipeline() pipeline.Pipeline {
	return v0.instance.PrePrepareMsgPipelineV0()
}

// PrepareMsgPipeline is the full processing msg pipeline for a prepare msg
func (v0 *testFork) PrepareMsgPipeline() pipeline.Pipeline {
	return v0.instance.PrepareMsgPipelineV0()
}

// CommitMsgValidationPipeline is a msg validation ONLY pipeline
func (v0 *testFork) CommitMsgValidationPipeline() pipeline.Pipeline {
	return v0.instance.CommitMsgValidationPipelineV0()
}

// CommitMsgPipeline is the full processing msg pipeline for a commit msg
func (v0 *testFork) CommitMsgPipeline() pipeline.Pipeline {
	return v0.instance.CommitMsgPipelineV0()
}

// DecidedMsgPipeline is a specific full processing pipeline for a decided msg
func (v0 *testFork) DecidedMsgPipeline() pipeline.Pipeline {
	return v0.instance.DecidedMsgPipelineV0()
}

// changeRoundMsgValidationPipeline is a msg validation ONLY pipeline for a change round msg
func (v0 *testFork) ChangeRoundMsgValidationPipeline() pipeline.Pipeline {
	return v0.instance.ChangeRoundMsgValidationPipelineV0()
}

// ChangeRoundMsgPipeline is the full processing msg pipeline for a change round msg
func (v0 *testFork) ChangeRoundMsgPipeline() pipeline.Pipeline {
	return v0.instance.ChangeRoundMsgPipelineV0()
}

func testingFork(instance *Instance) *testFork {
	return &testFork{instance: instance}
}

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
		PrepareMessages: msgcontinmem.New(3, 2),
		Config:          proto.DefaultConsensusParams(),
		ValidatorShare:  &keymanager.Share{Committee: nodes},
		state: &proto.State{
			Round:         threadsafe.Uint64(1),
			PreparedRound: threadsafe.Uint64(0),
			PreparedValue: threadsafe.Bytes(nil),
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
	instance.State().PreparedRound.Set(1)
	instance.State().PreparedValue.Set([]byte("value"))

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
		Config:         proto.DefaultConsensusParams(),
		ValidatorShare: &keymanager.Share{Committee: nodes},
		state: &proto.State{
			Round:         threadsafe.Uint64(1),
			PreparedRound: threadsafe.Uint64(0),
			PreparedValue: threadsafe.Bytes(nil),
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

			err = changeround.Validate(instance.ValidatorShare).Run(&proto.SignedMessage{
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
		ChangeRoundMessages: msgcontinmem.New(3, 2),
		Config:              proto.DefaultConsensusParams(),
		ValidatorShare: &keymanager.Share{Committee: map[uint64]*proto.Node{
			0: {IbftId: 0},
			1: {IbftId: 1},
			2: {IbftId: 2},
			3: {IbftId: 3},
		}},
		state: &proto.State{
			Round:         threadsafe.Uint64(1),
			PreparedRound: threadsafe.Uint64(0),
			PreparedValue: threadsafe.Bytes(nil),
		},
	}

	t.Run("no previous prepared", func(t *testing.T) {
		// test no previous prepared round and no round change quorum
		err := instance.JustifyRoundChange(2)
		require.NoError(t, err)
	})

	t.Run("change round quorum no previous prepare", func(t *testing.T) {
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
		err := instance.JustifyRoundChange(2)
		require.NoError(t, err)
	})

	t.Run("change round quorum not prepared, instance prepared previously", func(t *testing.T) {
		instance.State().PreparedRound.Set(1)
		instance.State().PreparedValue.Set([]byte("hello"))
		err := instance.JustifyRoundChange(2)
		require.EqualError(t, err, "highest prepared doesn't match prepared state")
	})

	t.Run("change round quorum prepared, instance prepared", func(t *testing.T) {
		instance.ChangeRoundMessages = msgcontinmem.New(3, 2)
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
		err := instance.JustifyRoundChange(2)
		require.NoError(t, err)
	})
}

func TestHighestPrepared(t *testing.T) {
	inputValue := []byte("input value")

	instance := &Instance{
		ChangeRoundMessages: msgcontinmem.New(3, 2),
		Config:              proto.DefaultConsensusParams(),
		ValidatorShare: &keymanager.Share{Committee: map[uint64]*proto.Node{
			0: {IbftId: 0},
			1: {IbftId: 1},
			2: {IbftId: 2},
			3: {IbftId: 3},
		}},
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
	notPrepared, highest, err := instance.HighestPrepared(3)
	require.NoError(t, err)
	require.False(t, notPrepared)
	require.EqualValues(t, 2, highest.PreparedRound)
	require.EqualValues(t, append(inputValue, []byte("highest")...), highest.PreparedValue)

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
	notPrepared, highest, err = instance.HighestPrepared(3)
	require.NoError(t, err)
	require.False(t, notPrepared)
	require.EqualValues(t, 2, highest.PreparedRound)
	require.EqualValues(t, append(inputValue, []byte("highest")...), highest.PreparedValue)
}

func TestChangeRoundMsgValidationPipeline(t *testing.T) {
	sks, nodes := GenerateNodes(4)

	tests := []struct {
		name          string
		msg           *proto.SignedMessage
		expectedError string
	}{
		{
			"valid",
			SignMsg(t, 1, sks[1], &proto.Message{
				Type:      proto.RoundState_ChangeRound,
				Round:     1,
				Lambda:    []byte("lambda"),
				SeqNumber: 1,
				Value: changeRoundDataToBytes(&proto.ChangeRoundData{
					PreparedValue: nil,
				}),
			}),
			"",
		},
		{
			"invalid change round data",
			SignMsg(t, 1, sks[1], &proto.Message{
				Type:      proto.RoundState_ChangeRound,
				Round:     1,
				Lambda:    []byte("lambda"),
				SeqNumber: 1,
				Value: changeRoundDataToBytes(&proto.ChangeRoundData{
					PreparedValue: []byte("ad"),
				}),
			}),
			"change round justification msg is nil",
		},
		{
			"invalid seq number",
			SignMsg(t, 1, sks[1], &proto.Message{
				Type:      proto.RoundState_ChangeRound,
				Round:     1,
				Lambda:    []byte("lambda"),
				SeqNumber: 2,
				Value: changeRoundDataToBytes(&proto.ChangeRoundData{
					PreparedValue: nil,
				}),
			}),
			"invalid message sequence number: expected: 1, actual: 2",
		},

		{
			"invalid lambda",
			SignMsg(t, 1, sks[1], &proto.Message{
				Type:      proto.RoundState_ChangeRound,
				Round:     1,
				Lambda:    []byte("lambdaa"),
				SeqNumber: 1,
				Value: changeRoundDataToBytes(&proto.ChangeRoundData{
					PreparedValue: nil,
				}),
			}),
			"message Lambda (lambdaa) does not equal expected Lambda (lambda)",
		},
		{
			"valid with different round",
			SignMsg(t, 1, sks[1], &proto.Message{
				Type:      proto.RoundState_ChangeRound,
				Round:     4,
				Lambda:    []byte("lambda"),
				SeqNumber: 1,
				Value: changeRoundDataToBytes(&proto.ChangeRoundData{
					PreparedValue: nil,
				}),
			}),
			"",
		},
		{
			"invalid msg type",
			SignMsg(t, 1, sks[1], &proto.Message{
				Type:      proto.RoundState_Decided,
				Round:     1,
				Lambda:    []byte("lambda"),
				SeqNumber: 1,
				Value: changeRoundDataToBytes(&proto.ChangeRoundData{
					PreparedValue: nil,
				}),
			}),
			"message type is wrong",
		},
	}

	instance := &Instance{
		Config: proto.DefaultConsensusParams(),
		ValidatorShare: &keymanager.Share{
			Committee: nodes,
			PublicKey: sks[1].GetPublicKey(), // just placeholder
		},
		state: &proto.State{
			Round:     threadsafe.Uint64(1),
			SeqNumber: threadsafe.Uint64(1),
			Lambda:    threadsafe.BytesS("lambda"),
		},
	}
	instance.fork = testingFork(instance)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := instance.ChangeRoundMsgValidationPipeline().Run(test.msg)
			if len(test.expectedError) > 0 {
				require.EqualError(t, err, test.expectedError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestChangeRoundFullQuorumPipeline(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	instance := &Instance{
		PrepareMessages: msgcontinmem.New(3, 2),
		Config:          proto.DefaultConsensusParams(),
		ValidatorShare: &keymanager.Share{
			Committee: nodes,
			PublicKey: sks[1].GetPublicKey(), // just placeholder
		},
		state: &proto.State{
			Round:     threadsafe.Uint64(1),
			Lambda:    threadsafe.Bytes(nil),
			SeqNumber: threadsafe.Uint64(0),
		},
	}
	pipeline := instance.changeRoundFullQuorumMsgPipeline()
	require.EqualValues(t, "if first pipeline non error, continue to second", pipeline.Name())
}

func TestChangeRoundPipeline(t *testing.T) {
	sks, nodes := GenerateNodes(4)
	instance := &Instance{
		PrepareMessages: msgcontinmem.New(3, 2),
		Config:          proto.DefaultConsensusParams(),
		ValidatorShare: &keymanager.Share{
			Committee: nodes,
			PublicKey: sks[1].GetPublicKey(), // just placeholder
		},
		state: &proto.State{
			Round:     threadsafe.Uint64(1),
			Lambda:    threadsafe.Bytes(nil),
			SeqNumber: threadsafe.Uint64(0),
		},
	}
	instance.fork = testingFork(instance)
	pipeline := instance.ChangeRoundMsgPipeline()
	require.EqualValues(t, "combination of: combination of: basic msg validation, type check, lambda, sequence, authorize, validateJustification msg, , add change round msg, upon change round partial quorum, if first pipeline non error, continue to second, ", pipeline.Name())
}
