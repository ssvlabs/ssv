package instance
//
//import (
//	"encoding/json"
//	"github.com/bloxapp/ssv/ibft/proto"
//	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
//	"go.uber.org/atomic"
//	"testing"
//
//	"github.com/bloxapp/ssv/protocol/v1/message"
//	"github.com/bloxapp/ssv/protocol/v1/qbft"
//	msgcontinmem "github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont/inmem"
//	"github.com/bloxapp/ssv/protocol/v1/qbft/validation"
//	changeround2 "github.com/bloxapp/ssv/protocol/v1/qbft/validation/changeround"
//	"github.com/bloxapp/ssv/utils/threshold"
//
//	"github.com/herumi/bls-eth-go-binary/bls"
//	"github.com/stretchr/testify/require"
//)
//
//type testFork struct {
//	instance *Instance
//}
//
//func (v0 *testFork) Apply(instance Instance) {
//
//}
//
//// PrePrepareMsgPipelineV0 is the full processing msg pipeline for a pre-prepare msg
//func (v0 *testFork) PrePrepareMsgPipeline() validation.SignedMessagePipeline {
//	return v0.instance.PrePrepareMsgPipelineV0()
//}
//
//// PrepareMsgPipeline is the full processing msg pipeline for a prepare msg
//func (v0 *testFork) PrepareMsgPipeline() validation.SignedMessagePipeline {
//	return v0.instance.PrepareMsgPipelineV0()
//}
//
//// CommitMsgValidationPipeline is a msg validation ONLY pipeline
//func (v0 *testFork) CommitMsgValidationPipeline() validation.SignedMessagePipeline {
//	return v0.instance.CommitMsgValidationPipelineV0()
//}
//
//// CommitMsgPipeline is the full processing msg pipeline for a commit msg
//func (v0 *testFork) CommitMsgPipeline() validation.SignedMessagePipeline {
//	return v0.instance.CommitMsgPipelineV0()
//}
//
//// DecidedMsgPipeline is a specific full processing pipeline for a decided msg
//func (v0 *testFork) DecidedMsgPipeline() validation.SignedMessagePipeline {
//	return v0.instance.DecidedMsgPipelineV0()
//}
//
//// changeRoundMsgValidationPipeline is a msg validation ONLY pipeline for a change round msg
//func (v0 *testFork) ChangeRoundMsgValidationPipeline() validation.SignedMessagePipeline {
//	return v0.instance.ChangeRoundMsgValidationPipelineV0()
//}
//
//// ChangeRoundMsgPipeline is the full processing msg pipeline for a change round msg
//func (v0 *testFork) ChangeRoundMsgPipeline() validation.SignedMessagePipeline {
//	return v0.instance.ChangeRoundMsgPipelineV0()
//}
//
//func testingFork(instance *Instance) *testFork {
//	return &testFork{instance: instance}
//}
//
//func changeRoundDataToBytes(input *message.RoundChangeData) []byte {
//	ret, _ := json.Marshal(input)
//	return ret
//}
//func bytesToChangeRoundData(input []byte) *message.RoundChangeData {
//	ret := &message.RoundChangeData{}
//	json.Unmarshal(input, ret)
//	return ret
//}
//
//// GenerateNodes generates randomly nodes
//func GenerateNodes(cnt int) (map[uint64]*bls.SecretKey, map[message.OperatorID]*beacon.Node) {
//	_ = bls.Init(bls.BLS12_381)
//	nodes := make(map[message.OperatorID]*beacon.Node)
//	sks := make(map[uint64]*bls.SecretKey)
//	for i := 1; i <= cnt; i++ {
//		sk := &bls.SecretKey{}
//		sk.SetByCSPRNG()
//
//		nodes[message.OperatorID(uint64(i))] = &beacon.Node{
//			IbftID: uint64(i),
//			Pk:     sk.GetPublicKey().Serialize(),
//		}
//		sks[uint64(i)] = sk
//	}
//	return sks, nodes
//}
//
//// SignMsg signs the given message by the given private key
//func SignMsg(t *testing.T, id uint64, sk *bls.SecretKey, msg *message.ConsensusMessage) *message.SignedMessage {
//	sigType := message.QBFTSigType
//	domain := message.ComputeSignatureDomain(message.PrimusTestnet, sigType)
//	sigRoot, err := message.ComputeSigningRoot(msg, domain)
//	require.NoError(t, err)
//	sig := sk.SignByte(sigRoot)
//
//	return &message.SignedMessage{
//		Message:   msg,
//		Signers:   []message.OperatorID{message.OperatorID(id)},
//		Signature: sig.Serialize(),
//	}
//}
//
//func TestRoundChangeInputValue(t *testing.T) {
//	secretKey, nodes := GenerateNodes(4)
//
//	round := atomic.Value{}
//	round.Store(message.Round(1))
//	preparedRound := atomic.Value{}
//	preparedRound.Store(message.Round(0))
//
//	instance := &Instance{
//		PrepareMessages: msgcontinmem.New(3, 2),
//		Config:          qbft.DefaultConsensusParams(),
//		ValidatorShare:  &beacon.Share{Committee: nodes},
//		state: &qbft.State{
//			Round:         round,
//			PreparedRound: preparedRound,
//			PreparedValue: atomic.Value{},
//		},
//	}
//
//	// no prepared round
//	byts, err := instance.roundChangeInputValue()
//	require.NoError(t, err)
//	require.NotNil(t, byts)
//	noPrepareChangeRoundData := message.RoundChangeData{}
//	require.NoError(t, json.Unmarshal(byts, &noPrepareChangeRoundData))
//	require.Nil(t, noPrepareChangeRoundData.PreparedValue)
//	require.EqualValues(t, uint64(0), noPrepareChangeRoundData.GetPreparedRound())
//	require.Nil(t, noPrepareChangeRoundData.GetRoundChangeJustification()[0].Message)
//	require.Nil(t, noPrepareChangeRoundData.GetRoundChangeJustification()[0].GetSignature())
//	require.Len(t, noPrepareChangeRoundData.GetRoundChangeJustification()[0].GetSigners(), 0)
//
//	// add votes
//	instance.PrepareMessages.AddMessage(SignMsg(t, 1, secretKey[1], &message.ConsensusMessage{
//		MsgType:    message.PrepareMsgType,
//		Height:     1,
//		Round:      1,
//		Identifier: []byte("Lambda"),
//		Data:       []byte("value"),
//	}))
//
//	instance.PrepareMessages.AddMessage(SignMsg(t, 1, secretKey[2], &message.ConsensusMessage{
//		MsgType:    message.PrepareMsgType,
//		Height:     1,
//		Round:      1,
//		Identifier: []byte("Lambda"),
//		Data:       []byte("value"),
//	}))
//
//	// with some prepare votes but not enough
//	byts, err = instance.roundChangeInputValue()
//	require.NoError(t, err)
//	require.NotNil(t, byts)
//	noPrepareChangeRoundData = message.RoundChangeData{}
//	require.NoError(t, json.Unmarshal(byts, &noPrepareChangeRoundData))
//	require.Nil(t, noPrepareChangeRoundData.PreparedValue)
//	require.EqualValues(t, uint64(0), noPrepareChangeRoundData.GetPreparedRound())
//	require.Nil(t, noPrepareChangeRoundData.GetRoundChangeJustification())
//	require.Nil(t, noPrepareChangeRoundData.GetRoundChangeJustification())
//	require.Len(t, noPrepareChangeRoundData.GetRoundChangeJustification(), 0)
//
//	// add more votes
//	instance.PrepareMessages.AddMessage(SignMsg(t, 3, secretKey[3], &message.ConsensusMessage{
//		MsgType:    message.PrepareMsgType,
//		Height:     1,
//		Round:      1,
//		Identifier: []byte("Lambda"),
//		Data:       []byte("value"),
//	}))
//	instance.State().PreparedRound.Store(message.Round(1))
//	instance.State().PreparedValue.Store([]byte("value"))
//
//	// with a prepared round
//	byts, err = instance.roundChangeInputValue()
//	require.NoError(t, err)
//	require.NotNil(t, byts)
//	data := bytesToChangeRoundData(byts)
//	require.EqualValues(t, 1, data.GetPreparedRound())
//	require.EqualValues(t, []byte("value"), data.PreparedValue)
//}
//
//func TestValidateChangeRoundMessage(t *testing.T) {
//	secretKeys, nodes := GenerateNodes(4)
//
//	round := atomic.Value{}
//	round.Store(message.Round(1))
//	preparedRound := atomic.Value{}
//	preparedRound.Store(message.Round(0))
//
//	instance := &Instance{
//		Config:         qbft.DefaultConsensusParams(),
//		ValidatorShare: &beacon.Share{Committee: nodes},
//		state: &qbft.State{
//			Round:         round,
//			PreparedRound: preparedRound,
//			PreparedValue: atomic.Value{},
//		},
//	}
//
//	tests := []struct {
//		name                string
//		msg                 *message.ConsensusMessage
//		signerID            uint64
//		justificationSigIds []uint64
//		expectedError       string
//	}{
//		{
//			name:     "valid",
//			signerID: 1,
//			msg: &message.ConsensusMessage{
//				MsgType:    message.RoundChangeMsgType,
//				Round:      1,
//				Identifier: []byte("Lambda"),
//				Data:       changeRoundDataToBytes(&message.RoundChangeData{}),
//			},
//			expectedError: "",
//		},
//		{
//			name:     "valid",
//			signerID: 1,
//			msg: &message.ConsensusMessage{
//				MsgType:    message.RoundChangeMsgType,
//				Round:      2,
//				Identifier: []byte("Lambda"),
//				Data:       changeRoundDataToBytes(&message.RoundChangeData{}),
//			},
//			expectedError: "",
//		},
//		{
//			name:     "valid",
//			signerID: 1,
//			msg: &message.ConsensusMessage{
//				MsgType:    message.RoundChangeMsgType,
//				Round:      3,
//				Identifier: []byte("Lambda"),
//				Data:       changeRoundDataToBytes(&message.RoundChangeData{}),
//			},
//			expectedError: "",
//		},
//		{
//			name:     "valid",
//			signerID: 1,
//			msg: &message.ConsensusMessage{
//				MsgType:    message.RoundChangeMsgType,
//				Round:      3,
//				Identifier: []byte("Lambda"),
//				Data:       changeRoundDataToBytes(&message.RoundChangeData{}),
//			},
//			expectedError: "",
//		},
//		{
//			name:     "nil ChangeRoundData",
//			signerID: 1,
//			msg: &message.ConsensusMessage{
//				MsgType:    message.RoundChangeMsgType,
//				Round:      3,
//				Identifier: []byte("Lambda"),
//				Data:       nil,
//			},
//			expectedError: "change round justification msg is nil",
//		},
//		{
//			name:                "valid justification",
//			signerID:            1,
//			justificationSigIds: []uint64{1, 2, 3},
//			msg: &message.ConsensusMessage{
//				MsgType:    message.RoundChangeMsgType,
//				Round:      3,
//				Identifier: []byte("Lambda"),
//				Data: changeRoundDataToBytes(&message.RoundChangeData{
//					PreparedValue:    []byte("value"),
//					Round:            message.Round(2),
//					NextProposalData: []byte("value"),
//					RoundChangeJustification: []*message.SignedMessage{
//						{
//							Signature: nil,
//							Signers:   []message.OperatorID{1, 2, 3},
//							Message: &message.ConsensusMessage{
//								MsgType:    message.PrepareMsgType,
//								Height:     0,
//								Round:      2,
//								Identifier: []byte("lambdas"),
//								Data:       []byte("value"),
//							},
//						},
//					},
//				}),
//			},
//			expectedError: "",
//		},
//		{
//			name:                "invalid justification msg type",
//			signerID:            1,
//			justificationSigIds: []uint64{1, 2, 3},
//			msg: &message.ConsensusMessage{
//				MsgType:    message.RoundChangeMsgType,
//				Round:      3,
//				Identifier: []byte("Lambda"),
//				Data: changeRoundDataToBytes(&message.RoundChangeData{
//					PreparedValue:    []byte("value"),
//					Round:            message.Round(2),
//					NextProposalData: []byte("value"),
//					RoundChangeJustification: []*message.SignedMessage{
//						{
//							Signature: nil,
//							Signers:   []message.OperatorID{1, 2, 3},
//							Message: &message.ConsensusMessage{
//								MsgType:    message.ProposalMsgType,
//								Height:     0,
//								Round:      2,
//								Identifier: []byte("lambdas"),
//								Data:       []byte("value"),
//							},
//						},
//					},
//				}),
//			},
//			expectedError: "change round justification msg type not Prepare",
//		},
//		{
//			name:                "invalid justification round",
//			signerID:            1,
//			justificationSigIds: []uint64{1, 2, 3},
//			msg: &message.ConsensusMessage{
//				MsgType:    message.RoundChangeMsgType,
//				Round:      3,
//				Identifier: []byte("Lambda"),
//				Data: changeRoundDataToBytes(&message.RoundChangeData{
//					PreparedValue:    []byte("value"),
//					Round:            message.Round(2),
//					NextProposalData: []byte("value"),
//					RoundChangeJustification: []*message.SignedMessage{
//						{
//							Signature: nil,
//							Signers:   []message.OperatorID{1, 2, 3},
//							Message: &message.ConsensusMessage{
//								MsgType:    message.PrepareMsgType,
//								Height:     0,
//								Round:      3,
//								Identifier: []byte("lambdas"),
//								Data:       []byte("value"),
//							},
//						},
//					},
//				}),
//			},
//			expectedError: "change round justification round lower or equal to message round",
//		},
//		{
//			name:                "invalid prepared and justification round",
//			signerID:            1,
//			justificationSigIds: []uint64{1, 2, 3},
//			msg: &message.ConsensusMessage{
//				MsgType:    message.RoundChangeMsgType,
//				Round:      3,
//				Identifier: []byte("Lambda"),
//				Data: changeRoundDataToBytes(&message.RoundChangeData{
//					PreparedValue:    []byte("value"),
//					Round:            message.Round(2),
//					NextProposalData: []byte("value"),
//					RoundChangeJustification: []*message.SignedMessage{
//						{
//							Signature: nil,
//							Signers:   []message.OperatorID{1, 2, 3},
//							Message: &message.ConsensusMessage{
//								MsgType:    message.PrepareMsgType,
//								Height:     0,
//								Round:      1,
//								Identifier: []byte("lambdas"),
//								Data:       []byte("value"),
//							},
//						},
//					},
//				}),
//			},
//			expectedError: "change round prepared round not equal to justification msg round",
//		},
//		{
//			name:                "invalid justification instance",
//			signerID:            1,
//			justificationSigIds: []uint64{1, 2, 3},
//			msg: &message.ConsensusMessage{
//				MsgType:    message.RoundChangeMsgType,
//				Round:      3,
//				Identifier: []byte("Lambda"),
//				Data: changeRoundDataToBytes(&message.RoundChangeData{
//					PreparedValue:    []byte("value"),
//					Round:            message.Round(2),
//					NextProposalData: []byte("value"),
//					RoundChangeJustification: []*message.SignedMessage{
//						{
//							Signature: nil,
//							Signers:   []message.OperatorID{1, 2, 3},
//							Message: &message.ConsensusMessage{
//								MsgType:    message.PrepareMsgType,
//								Height:     0,
//								Round:      2,
//								Identifier: []byte("lambdas"),
//								Data:       []byte("value"),
//							},
//						},
//					},
//				}),
//			},
//			expectedError: "change round justification msg Lambda not equal to msg Lambda not equal to instance lambda",
//		},
//		{
//			name:                "invalid justification quorum",
//			signerID:            1,
//			justificationSigIds: []uint64{1, 2},
//			msg: &message.ConsensusMessage{
//				MsgType:    message.RoundChangeMsgType,
//				Round:      3,
//				Identifier: []byte("Lambda"),
//				Data: changeRoundDataToBytes(&message.RoundChangeData{
//					PreparedValue:    []byte("value"),
//					Round:            message.Round(2),
//					NextProposalData: []byte("value"),
//					RoundChangeJustification: []*message.SignedMessage{
//						{
//							Signature: nil,
//							Signers:   []message.OperatorID{1, 2},
//							Message: &message.ConsensusMessage{
//								MsgType:    message.PrepareMsgType,
//								Height:     0,
//								Round:      2,
//								Identifier: []byte("lambdas"),
//								Data:       []byte("value"),
//							},
//						},
//					},
//				}),
//			},
//			expectedError: "change round justification does not constitute a quorum",
//		},
//		{
//			name:                "valid justification",
//			signerID:            1,
//			justificationSigIds: []uint64{1, 2, 3},
//			msg: &message.ConsensusMessage{
//				MsgType:    message.RoundChangeMsgType,
//				Round:      3,
//				Identifier: []byte("Lambda"),
//				Data: changeRoundDataToBytes(&message.RoundChangeData{
//					PreparedValue:    []byte("value"),
//					Round:            message.Round(2),
//					NextProposalData: []byte("value"),
//					RoundChangeJustification: []*message.SignedMessage{
//						{
//							Signature: nil,
//							Signers:   []message.OperatorID{1, 2, 3},
//							Message: &message.ConsensusMessage{
//								MsgType:    message.PrepareMsgType,
//								Height:     0,
//								Round:      2,
//								Identifier: []byte("lambdas"),
//								Data:       []byte("value"),
//							},
//						},
//					},
//				}),
//			},
//			expectedError: "change round prepared value not equal to justification msg value",
//		},
//		{
//			name:                "invalid justification sig",
//			signerID:            1,
//			justificationSigIds: []uint64{1, 2},
//			msg: &message.ConsensusMessage{
//				MsgType:    message.RoundChangeMsgType,
//				Round:      3,
//				Identifier: []byte("Lambda"),
//				Data: changeRoundDataToBytes(&message.RoundChangeData{
//					PreparedValue:    []byte("value"),
//					Round:            message.Round(2),
//					NextProposalData: []byte("value"),
//					RoundChangeJustification: []*message.SignedMessage{
//						{
//							Signature: nil,
//							Signers:   []message.OperatorID{1, 2, 3},
//							Message: &message.ConsensusMessage{
//								MsgType:    message.PrepareMsgType,
//								Height:     0,
//								Round:      2,
//								Identifier: []byte("lambdas"),
//								Data:       []byte("value"),
//							},
//						},
//					},
//				}),
//			},
//			expectedError: "change round justification signature doesn't verify",
//		},
//	}
//
//	threshold.Init()
//	for _, test := range tests {
//		t.Run(test.name, func(t *testing.T) {
//			// sign if needed
//			roundChangeData, err := test.msg.GetRoundChangeData()
//			require.NoError(t, err)
//			if roundChangeData != nil {
//				var signature *bls.Sign
//				data, _ := test.msg.GetRoundChangeData()
//				if len(test.justificationSigIds) > 0 {
//					for _, id := range test.justificationSigIds {
//						sign, err := data.GetRoundChangeJustification().Sign(secretKeys[id])
//						require.NoError(t, err)
//						if signature == nil {
//							signature = sign
//						} else {
//							signature.Add(sign)
//						}
//					}
//					data.RoundChangeJustification = signature.Serialize()
//					test.msg.Value = changeRoundDataToBytes(data)
//				}
//			}
//
//			signature, err := test.msg.Sign(secretKeys[test.signerID])
//			require.NoError(t, err)
//
//			_, err = changeround2.Validate(instance.ValidatorShare).Run(&message.SignedMessage{
//				Signature: signature.Serialize(),
//				Signers:   []message.OperatorID{message.OperatorID(test.signerID)},
//				Message:   test.msg,
//			})
//			if len(test.expectedError) > 0 {
//				require.EqualError(t, err, test.expectedError)
//			} else {
//				require.NoError(t, err)
//			}
//		})
//	}
//}
//
//func TestRoundChangeJustification(t *testing.T) {
//	inputValue := changeRoundDataToBytes(&message.RoundChangeData{
//		PreparedValue:            []byte("hello"),
//		Round:                    1,
//		NextProposalData:         nil,
//		RoundChangeJustification: nil,
//	})
//
//	round := atomic.Value{}
//	round.Store(message.Round(1))
//	preparedRound := atomic.Uint64{}
//	preparedRound.Store(0)
//
//	instance := &Instance{
//		ChangeRoundMessages: msgcontinmem.New(3, 2),
//		Config:              proto.DefaultConsensusParams(),
//		ValidatorShare: &beacon.Share{Committee: map[message.OperatorID]*beacon.Node{
//			0: {IbftId: 0},
//			1: {IbftId: 1},
//			2: {IbftId: 2},
//			3: {IbftId: 3},
//		}},
//		state: &qbft.State{
//			Round:         round,
//			PreparedRound: preparedRound,
//			PreparedValue: atomic.String{},
//		},
//	}
//
//	t.Run("no previous prepared", func(t *testing.T) {
//		// test no previous prepared round and no round change quorum
//		err := instance.JustifyRoundChange(2)
//		require.NoError(t, err)
//	})
//
//	t.Run("change round quorum no previous prepare", func(t *testing.T) {
//		instance.ChangeRoundMessages.AddMessage(&message.SignedMessage{
//			Signature: nil,
//			Signers:   []message.OperatorID{message.OperatorID(1)},
//			Message: &message.ConsensusMessage{
//				MsgType:    message.RoundChangeMsgType,
//				Height:     1,
//				Round:      2,
//				Identifier: []byte("Lambda"),
//				Data:       []byte("data"), /*changeRoundDataToBytes(&message.RoundChangeData{})*/
//			}})
//		instance.ChangeRoundMessages.AddMessage(&message.SignedMessage{
//			Signature: nil,
//			Signers:   []message.OperatorID{message.OperatorID(2)},
//			Message: &message.ConsensusMessage{
//				MsgType:    message.RoundChangeMsgType,
//				Height:     1,
//				Round:      2,
//				Identifier: []byte("Lambda"),
//				Data:       []byte("data"), /*changeRoundDataToBytes(&message.RoundChangeData{})*/
//			}})
//		instance.ChangeRoundMessages.AddMessage(&message.SignedMessage{
//			Signature: nil,
//			Signers:   []message.OperatorID{message.OperatorID(3)},
//			Message: &message.ConsensusMessage{
//				MsgType:    message.RoundChangeMsgType,
//				Height:     1,
//				Round:      2,
//				Identifier: []byte("Lambda"),
//				Data:       []byte("data"), /*changeRoundDataToBytes(&message.RoundChangeData{})*/
//			}})
//
//		// test no previous prepared round with round change quorum (no justification)
//		err := instance.JustifyRoundChange(2)
//		require.NoError(t, err)
//	})
//
//	t.Run("change round quorum not prepared, instance prepared previously", func(t *testing.T) {
//		instance.State().PreparedRound.Set(1)
//		instance.State().PreparedValue.Set([]byte("hello"))
//		err := instance.JustifyRoundChange(2)
//		require.EqualError(t, err, "highest prepared doesn't match prepared state")
//	})
//
//	t.Run("change round quorum prepared, instance prepared", func(t *testing.T) {
//		instance.ChangeRoundMessages = msgcontinmem.New(3, 2)
//		instance.ChangeRoundMessages.AddMessage(&message.SignedMessage{
//			Signature: nil,
//			Signers:   []message.OperatorID{message.OperatorID(1)},
//			Message: &message.ConsensusMessage{
//				MsgType:    message.RoundChangeMsgType,
//				Height:     1,
//				Round:      2,
//				Identifier: []byte("Lambda"),
//				Data:       inputValue,
//			}})
//		instance.ChangeRoundMessages.AddMessage(&message.SignedMessage{
//			Signature: nil,
//			Signers:   []message.OperatorID{message.OperatorID(2)},
//			Message: &message.ConsensusMessage{
//				MsgType:    message.RoundChangeMsgType,
//				Height:     1,
//				Round:      2,
//				Identifier: []byte("Lambda"),
//				Data:       inputValue,
//			}})
//		instance.ChangeRoundMessages.AddMessage(&message.SignedMessage{
//			Signature: nil,
//			Signers:   []message.OperatorID{message.OperatorID(3)},
//			Message: &message.ConsensusMessage{
//				MsgType:    message.RoundChangeMsgType,
//				Height:     1,
//				Round:      1,
//				Identifier: []byte("Lambda"),
//				Data:       inputValue,
//			}})
//		// test no previous prepared round with round change quorum (with justification)
//		err := instance.JustifyRoundChange(2)
//		require.NoError(t, err)
//	})
//}
//
//func TestHighestPrepared(t *testing.T) {
//	inputValue := []byte("input value")
//
//	instance := &Instance{
//		ChangeRoundMessages: msgcontinmem.New(3, 2),
//		Config:              proto.DefaultConsensusParams(),
//		ValidatorShare: &beacon.Share{Committee: map[message.OperatorID]*beacon.Node{
//			0: {IbftId: 0},
//			1: {IbftId: 1},
//			2: {IbftId: 2},
//			3: {IbftId: 3},
//		}},
//	}
//
//	instance.ChangeRoundMessages.AddMessage(&message.SignedMessage{
//		Signature: nil,
//		Signers:   []message.OperatorID{message.OperatorID(1)},
//		Message: &message.ConsensusMessage{
//			MsgType:    message.RoundChangeMsgType,
//			Height:     1,
//			Round:      3,
//			Identifier: []byte("Lambda"),
//			Data:       []byte("data"), /*changeRoundDataToBytes(&message.RoundChangeData{PreparedRound: 1,PreparedValue: inputValue,})*/
//		}})
//	instance.ChangeRoundMessages.AddMessage(&message.SignedMessage{
//		Signature: nil,
//		Signers:   []message.OperatorID{message.OperatorID(2)},
//		Message: &message.ConsensusMessage{
//			MsgType:    message.RoundChangeMsgType,
//			Height:     1,
//			Round:      3,
//			Identifier: []byte("Lambda"),
//			Data:       []byte("data"), /*changeRoundDataToBytes(&message.RoundChangeData{PreparedRound: 2,PreparedValue: inputValue,})*/
//		}})
//
//	// test one higher than other
//	notPrepared, highest, err := instance.HighestPrepared(3)
//	require.NoError(t, err)
//	require.False(t, notPrepared)
//	require.EqualValues(t, 2, highest.PreparedRound)
//	require.EqualValues(t, append(inputValue, []byte("highest")...), highest.PreparedValue)
//
//	// test 2 equals
//	instance.ChangeRoundMessages.AddMessage(&message.SignedMessage{
//		Signature: nil,
//		Signers:   []message.OperatorID{message.OperatorID(2)},
//		Message: &message.ConsensusMessage{
//			MsgType:    message.RoundChangeMsgType,
//			Height:     1,
//			Round:      3,
//			Identifier: []byte("Lambda"),
//			Data:       []byte("data"), /*changeRoundDataToBytes(&message.RoundChangeData{PreparedRound: 2,PreparedValue: append(inputValue, []byte("highest")...)})*/
//		}})
//
//	notPrepared, highest, err = instance.HighestPrepared(3)
//	require.NoError(t, err)
//	require.False(t, notPrepared)
//	require.EqualValues(t, 2, highest.PreparedRound)
//	require.EqualValues(t, append(inputValue, []byte("highest")...), highest.PreparedValue)
//}
//
//func TestChangeRoundMsgValidationPipeline(t *testing.T) {
//	sks, nodes := GenerateNodes(4)
//
//	tests := []struct {
//		name          string
//		msg           *message.SignedMessage
//		expectedError string
//	}{
//		{
//			"valid",
//			SignMsg(t, 1, sks[1], &message.ConsensusMessage{
//				MsgType:    message.RoundChangeMsgType,
//				Height:     1,
//				Round:      1,
//				Identifier: []byte("Lambda"),
//				Data:       []byte("value"), /*changeRoundDataToBytes(&proto.ChangeRoundData{PreparedValue: nil,})*/
//			}),
//			"",
//		},
//		{
//			"invalid change round data",
//			SignMsg(t, 1, sks[1], &message.ConsensusMessage{
//				MsgType:    message.RoundChangeMsgType,
//				Height:     1,
//				Round:      1,
//				Identifier: []byte("Lambda"),
//				Data:       []byte("value"), /*changeRoundDataToBytes(&proto.ChangeRoundData{PreparedValue: []byte("ad")],})*/
//			}),
//			"change round justification msg is nil",
//		},
//		{
//			"invalid seq number",
//			SignMsg(t, 1, sks[1], &message.ConsensusMessage{
//				MsgType:    message.RoundChangeMsgType,
//				Height:     2,
//				Round:      1,
//				Identifier: []byte("Lambda"),
//				Data:       []byte("value"), /*changeRoundDataToBytes(&proto.ChangeRoundData{PreparedValue: nil,})*/
//			}),
//			"invalid message sequence number: expected: 1, actual: 2",
//		},
//
//		{
//			"invalid lambda",
//			SignMsg(t, 1, sks[1], &message.ConsensusMessage{
//				MsgType:    message.RoundChangeMsgType,
//				Height:     1,
//				Round:      1,
//				Identifier: []byte("Lambda"),
//				Data:       []byte("value"), /*changeRoundDataToBytes(&proto.ChangeRoundData{PreparedValue: nil,})*/
//			}),
//			"message Lambda (lambdaa) does not equal expected Lambda (lambda)",
//		},
//		{
//			"valid with different round",
//			SignMsg(t, 1, sks[1], &message.ConsensusMessage{
//				MsgType:    message.RoundChangeMsgType,
//				Height:     1,
//				Round:      4,
//				Identifier: []byte("Lambda"),
//				Data:       []byte("value"), /*changeRoundDataToBytes(&proto.ChangeRoundData{PreparedValue: nil,})*/
//			}),
//			"",
//		},
//		{
//			"invalid msg type",
//			SignMsg(t, 1, sks[1], &message.ConsensusMessage{
//				MsgType:    message.RoundChangeMsgType,
//				Height:     1,
//				Round:      1,
//				Identifier: []byte("Lambda"),
//				Data:       []byte("value"), /*changeRoundDataToBytes(&proto.ChangeRoundData{PreparedValue: nil,})*/
//			}),
//			"message type is wrong",
//		},
//	}
//
//	round := atomic.Value{}
//	round.Store(message.Round(1))
//	seq := atomic.Value{}
//	seq.Store(message.Height(1))
//	identifier := atomic.String{}
//	identifier.Store("lambda")
//
//	instance := &Instance{
//		Config: proto.DefaultConsensusParams(),
//		ValidatorShare: &beacon.Share{
//			Committee: nodes,
//			PublicKey: sks[1].GetPublicKey(), // just placeholder
//		},
//		state: &qbft.State{
//			Round:      round,
//			Height:     seq,
//			Identifier: identifier,
//		},
//	}
//	instance.fork = testingFork(instance)
//
//	for _, test := range tests {
//		t.Run(test.name, func(t *testing.T) {
//			_, err := instance.ChangeRoundMsgValidationPipeline().Run(test.msg)
//			if len(test.expectedError) > 0 {
//				require.EqualError(t, err, test.expectedError)
//			} else {
//				require.NoError(t, err)
//			}
//		})
//	}
//}
//
//func TestChangeRoundFullQuorumPipeline(t *testing.T) {
//	sks, nodes := GenerateNodes(4)
//
//	round := atomic.Value{}
//	round.Store(message.Round(1))
//	height := atomic.Value{}
//	height.Store(message.Height(1))
//
//	instance := &Instance{
//		PrepareMessages: msgcontinmem.New(3, 2),
//		Config:          proto.DefaultConsensusParams(),
//		ValidatorShare: &beacon.Share{
//			Committee: nodes,
//			PublicKey: sks[1].GetPublicKey(), // just placeholder
//		},
//		state: &qbft.State{
//			Round:      round,
//			Identifier: atomic.String{},
//			Height:     height,
//		},
//	}
//	pipeline := instance.changeRoundFullQuorumMsgPipeline()
//	require.EqualValues(t, "if first pipeline non error, continue to second", pipeline.Name())
//}
//
//func TestChangeRoundPipeline(t *testing.T) {
//	sks, nodes := GenerateNodes(4)
//
//	round := atomic.Value{}
//	round.Store(message.Round(1))
//	height := atomic.Value{}
//	height.Store(message.Height(1))
//
//	instance := &Instance{
//		PrepareMessages: msgcontinmem.New(3, 2),
//		Config:          proto.DefaultConsensusParams(),
//		ValidatorShare: &beacon.Share{
//			Committee: nodes,
//			PublicKey: sks[1].GetPublicKey(), // just placeholder
//		},
//		state: &qbft.State{
//			Round:      round,
//			Identifier: atomic.String{},
//			Height:     height,
//		},
//	}
//	instance.fork = testingFork(instance)
//	pipeline := instance.ChangeRoundMsgPipeline()
//	require.EqualValues(t, "combination of: combination of: basic msg validation, type check, lambda, sequence, authorize, validateJustification msg, , add change round msg, upon change round partial quorum, if first pipeline non error, continue to second, ", pipeline.Name())
//}
