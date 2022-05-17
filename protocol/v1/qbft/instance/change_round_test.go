package instance

import (
	"encoding/json"
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"go.uber.org/zap"

	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	msgcontinmem "github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont/inmem"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	changeround2 "github.com/bloxapp/ssv/protocol/v1/qbft/validation/changeround"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/preprepare"
	"github.com/bloxapp/ssv/utils/threshold"
)

type testFork struct {
	instance *Instance
}

func (v0 *testFork) VersionName() string {
	return "v0"
}

func (v0 *testFork) Apply(instance Instance) {

}

// PrePrepareMsgPipelineV0 is the full processing msg pipeline for a pre-prepare msg
func (v0 *testFork) PrePrepareMsgValidationPipeline(share *beacon.Share, state *qbft.State, roundLeader preprepare.LeaderResolver) pipelines.SignedMessagePipeline {
	return v0.instance.PrePrepareMsgPipeline()
}

// PrepareMsgPipeline is the full processing msg pipeline for a prepare msg
func (v0 *testFork) PrepareMsgValidationPipeline(share *beacon.Share, state *qbft.State) pipelines.SignedMessagePipeline {
	return v0.instance.PrepareMsgPipeline()
}

// CommitMsgValidationPipeline is a msg validation ONLY pipeline
func (v0 *testFork) CommitMsgValidationPipeline(share *beacon.Share, identifier message.Identifier, height message.Height) pipelines.SignedMessagePipeline {
	return v0.instance.CommitMsgValidationPipeline()
}

// CommitMsgPipeline is the full processing msg pipeline for a commit msg
func (v0 *testFork) CommitMsgPipeline() pipelines.SignedMessagePipeline {
	return v0.instance.CommitMsgPipeline()
}

// DecidedMsgPipeline is a specific full processing pipeline for a decided msg
func (v0 *testFork) DecidedMsgPipeline() pipelines.SignedMessagePipeline {
	return v0.instance.DecidedMsgPipeline()
}

// changeRoundMsgValidationPipeline is a msg validation ONLY pipeline for a change round msg
func (v0 *testFork) ChangeRoundMsgValidationPipeline(share *beacon.Share, identifier message.Identifier, height message.Height) pipelines.SignedMessagePipeline {
	return v0.instance.ChangeRoundMsgValidationPipeline()
}

// ChangeRoundMsgPipeline is the full processing msg pipeline for a change round msg
func (v0 *testFork) ChangeRoundMsgPipeline() pipelines.SignedMessagePipeline {
	return v0.instance.ChangeRoundMsgPipeline()
}

func testingFork(instance *Instance) *testFork {
	return &testFork{instance: instance}
}

func changeRoundDataToBytes(input *message.RoundChangeData) []byte {
	ret, _ := json.Marshal(input)
	return ret
}
func bytesToChangeRoundData(input []byte) *message.RoundChangeData {
	ret := &message.RoundChangeData{}
	json.Unmarshal(input, ret)
	return ret
}

// GenerateNodes generates randomly nodes
func GenerateNodes(cnt int) (map[uint64]*bls.SecretKey, map[message.OperatorID]*beacon.Node) {
	_ = bls.Init(bls.BLS12_381)
	nodes := make(map[message.OperatorID]*beacon.Node)
	sks := make(map[uint64]*bls.SecretKey)
	for i := 1; i <= cnt; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()

		nodes[message.OperatorID(uint64(i))] = &beacon.Node{
			IbftID: uint64(i),
			Pk:     sk.GetPublicKey().Serialize(),
		}
		sks[uint64(i)] = sk
	}
	return sks, nodes
}

// SignMsg signs the given message by the given private key
func SignMsg(t *testing.T, id uint64, sk *bls.SecretKey, msg *message.ConsensusMessage) *message.SignedMessage {
	sigType := message.QBFTSigType
	domain := message.ComputeSignatureDomain(message.PrimusTestnet, sigType)
	sigRoot, err := message.ComputeSigningRoot(msg, domain)
	require.NoError(t, err)
	sig := sk.SignByte(sigRoot)

	return &message.SignedMessage{
		Message:   msg,
		Signers:   []message.OperatorID{message.OperatorID(id)},
		Signature: sig.Serialize(),
	}
}

func TestRoundChangeInputValue(t *testing.T) {
	secretKey, nodes := GenerateNodes(4)
	round := atomic.Value{}
	round.Store(message.Round(1))

	instance := &Instance{
		Logger:          zap.L(),
		PrepareMessages: msgcontinmem.New(3, 2),
		Config:          qbft.DefaultConsensusParams(),
		ValidatorShare:  &beacon.Share{Committee: nodes},
		state: &qbft.State{
			Round: round,
		},
	}

	// no prepared round
	byts, err := instance.roundChangeInputValue()
	require.NoError(t, err)
	require.NotNil(t, byts)
	noPrepareChangeRoundData := message.RoundChangeData{}
	require.NoError(t, json.Unmarshal(byts, &noPrepareChangeRoundData))
	require.Nil(t, noPrepareChangeRoundData.PreparedValue)
	require.EqualValues(t, uint64(0), noPrepareChangeRoundData.GetPreparedRound())
	require.Nil(t, noPrepareChangeRoundData.GetRoundChangeJustification()[0].Message)
	require.Nil(t, noPrepareChangeRoundData.GetRoundChangeJustification()[0].GetSignature())
	require.Len(t, noPrepareChangeRoundData.GetRoundChangeJustification()[0].GetSigners(), 0)

	// add votes
	msg := &message.ConsensusMessage{
		MsgType:    message.PrepareMsgType,
		Height:     1,
		Round:      1,
		Identifier: []byte("Lambda"),
		Data:       prepareDataToBytes(&message.PrepareData{Data: []byte("value")}),
	}

	prepareData, err := msg.GetPrepareData()
	require.NoError(t, err)

	instance.PrepareMessages.AddMessage(SignMsg(t, 1, secretKey[1], msg), prepareData.Data)
	instance.PrepareMessages.AddMessage(SignMsg(t, 1, secretKey[2], msg), prepareData.Data)

	// with some prepare votes but not enough
	byts, err = instance.roundChangeInputValue()
	require.NoError(t, err)
	require.NotNil(t, byts)
	noPrepareChangeRoundData = message.RoundChangeData{}
	require.NoError(t, json.Unmarshal(byts, &noPrepareChangeRoundData))
	require.Nil(t, noPrepareChangeRoundData.PreparedValue)
	require.EqualValues(t, uint64(0), noPrepareChangeRoundData.GetPreparedRound())
	require.Nil(t, noPrepareChangeRoundData.GetRoundChangeJustification()[0].Message)
	require.Nil(t, noPrepareChangeRoundData.GetRoundChangeJustification()[0].GetSignature())
	require.Len(t, noPrepareChangeRoundData.GetRoundChangeJustification()[0].GetSigners(), 0)

	// add more votes
	instance.PrepareMessages.AddMessage(SignMsg(t, 3, secretKey[3], msg), prepareData.Data)
	instance.State().PreparedRound.Store(message.Round(1))
	instance.State().PreparedValue.Store([]byte("value"))

	// with a prepared round
	byts, err = instance.roundChangeInputValue()
	require.NoError(t, err)
	require.NotNil(t, byts)
	data := bytesToChangeRoundData(byts)
	require.EqualValues(t, 1, data.GetPreparedRound())
	require.EqualValues(t, []byte("value"), data.PreparedValue)
}

// TODO(nkryuchkov): fix this test
func TestValidateChangeRoundMessage(t *testing.T) {
	secretKeys, nodes := GenerateNodes(4)
	round := atomic.Value{}
	round.Store(message.Round(1))

	instance := &Instance{
		Config:         qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{Committee: nodes},
		state: &qbft.State{
			Round: round,
		},
	}

	tests := []struct {
		name                string
		msg                 *message.ConsensusMessage
		signerID            uint64
		justificationSigIds []uint64
		expectedError       string
	}{
		{
			name:     "valid",
			signerID: 1,
			msg: &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Round:      1,
				Identifier: []byte("Lambda"),
				Data:       changeRoundDataToBytes(&message.RoundChangeData{}),
			},
			expectedError: "",
		},
		{
			name:     "valid",
			signerID: 1,
			msg: &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Round:      2,
				Identifier: []byte("Lambda"),
				Data:       changeRoundDataToBytes(&message.RoundChangeData{}),
			},
			expectedError: "",
		},
		{
			name:     "valid",
			signerID: 1,
			msg: &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Round:      3,
				Identifier: []byte("Lambda"),
				Data:       changeRoundDataToBytes(&message.RoundChangeData{}),
			},
			expectedError: "",
		},
		{
			name:     "valid",
			signerID: 1,
			msg: &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Round:      3,
				Identifier: []byte("Lambda"),
				Data:       changeRoundDataToBytes(&message.RoundChangeData{}),
			},
			expectedError: "",
		},
		{
			name:     "nil ChangeRoundData",
			signerID: 1,
			msg: &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Round:      3,
				Identifier: []byte("Lambda"),
				Data:       nil,
			},
			expectedError: "change round justification msg is nil",
		},
		{
			name:                "valid justification",
			signerID:            1,
			justificationSigIds: []uint64{1, 2, 3},
			msg: &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Round:      3,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(&message.RoundChangeData{
					PreparedValue:    []byte("value"),
					Round:            message.Round(2),
					NextProposalData: []byte("value"),
					RoundChangeJustification: []*message.SignedMessage{
						{
							Signature: nil,
							Signers:   []message.OperatorID{1, 2, 3},
							Message: &message.ConsensusMessage{
								MsgType:    message.PrepareMsgType,
								Height:     0,
								Round:      2,
								Identifier: []byte("lambdas"),
								Data:       []byte("value"),
							},
						},
					},
				}),
			},
			expectedError: "",
		},
		{
			name:                "invalid justification msg type",
			signerID:            1,
			justificationSigIds: []uint64{1, 2, 3},
			msg: &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Round:      3,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(&message.RoundChangeData{
					PreparedValue:    []byte("value"),
					Round:            message.Round(2),
					NextProposalData: []byte("value"),
					RoundChangeJustification: []*message.SignedMessage{
						{
							Signature: nil,
							Signers:   []message.OperatorID{1, 2, 3},
							Message: &message.ConsensusMessage{
								MsgType:    message.ProposalMsgType,
								Height:     0,
								Round:      2,
								Identifier: []byte("lambdas"),
								Data:       []byte("value"),
							},
						},
					},
				}),
			},
			expectedError: "change round justification msg type not Prepare (0)",
		},
		{
			name:                "invalid justification round",
			signerID:            1,
			justificationSigIds: []uint64{1, 2, 3},
			msg: &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Round:      3,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(&message.RoundChangeData{
					PreparedValue:    []byte("value"),
					Round:            message.Round(2),
					NextProposalData: []byte("value"),
					RoundChangeJustification: []*message.SignedMessage{
						{
							Signature: nil,
							Signers:   []message.OperatorID{1, 2, 3},
							Message: &message.ConsensusMessage{
								MsgType:    message.PrepareMsgType,
								Height:     0,
								Round:      3,
								Identifier: []byte("lambdas"),
								Data:       []byte("value"),
							},
						},
					},
				}),
			},
			expectedError: "change round justification round lower or equal to message round",
		},
		{
			name:                "invalid prepared and justification round",
			signerID:            1,
			justificationSigIds: []uint64{1, 2, 3},
			msg: &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Round:      3,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(&message.RoundChangeData{
					PreparedValue:    []byte("value"),
					Round:            message.Round(2),
					NextProposalData: []byte("value"),
					RoundChangeJustification: []*message.SignedMessage{
						{
							Signature: nil,
							Signers:   []message.OperatorID{1, 2, 3},
							Message: &message.ConsensusMessage{
								MsgType:    message.PrepareMsgType,
								Height:     0,
								Round:      1,
								Identifier: []byte("lambdas"),
								Data:       []byte("value"),
							},
						},
					},
				}),
			},
			expectedError: "change round prepared round not equal to justification msg round",
		},
		{
			name:                "invalid justification instance",
			signerID:            1,
			justificationSigIds: []uint64{1, 2, 3},
			msg: &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Round:      3,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(&message.RoundChangeData{
					PreparedValue:    []byte("value"),
					Round:            message.Round(2),
					NextProposalData: []byte("value"),
					RoundChangeJustification: []*message.SignedMessage{
						{
							Signature: nil,
							Signers:   []message.OperatorID{1, 2, 3},
							Message: &message.ConsensusMessage{
								MsgType:    message.PrepareMsgType,
								Height:     0,
								Round:      2,
								Identifier: []byte("lambdas"),
								Data:       []byte("value"),
							},
						},
					},
				}),
			},
			expectedError: "change round justification msg Lambda not equal to msg Lambda not equal to instance lambda",
		},
		{
			name:                "invalid justification quorum",
			signerID:            1,
			justificationSigIds: []uint64{1, 2},
			msg: &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Round:      3,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(&message.RoundChangeData{
					PreparedValue:    []byte("value"),
					Round:            message.Round(2),
					NextProposalData: []byte("value"),
					RoundChangeJustification: []*message.SignedMessage{
						{
							Signature: nil,
							Signers:   []message.OperatorID{1, 2},
							Message: &message.ConsensusMessage{
								MsgType:    message.PrepareMsgType,
								Height:     0,
								Round:      2,
								Identifier: []byte("lambdas"),
								Data:       []byte("value"),
							},
						},
					},
				}),
			},
			expectedError: "change round justification does not constitute a quorum",
		},
		{
			name:                "valid justification",
			signerID:            1,
			justificationSigIds: []uint64{1, 2, 3},
			msg: &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Round:      3,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(&message.RoundChangeData{
					PreparedValue:    []byte("value"),
					Round:            message.Round(2),
					NextProposalData: []byte("value"),
					RoundChangeJustification: []*message.SignedMessage{
						{
							Signature: nil,
							Signers:   []message.OperatorID{1, 2, 3},
							Message: &message.ConsensusMessage{
								MsgType:    message.PrepareMsgType,
								Height:     0,
								Round:      2,
								Identifier: []byte("lambdas"),
								Data:       []byte("value"),
							},
						},
					},
				}),
			},
			expectedError: "change round prepared value not equal to justification msg value",
		},
		{
			name:                "invalid justification sig",
			signerID:            1,
			justificationSigIds: []uint64{1, 2},
			msg: &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Round:      3,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(&message.RoundChangeData{
					PreparedValue:    []byte("value"),
					Round:            message.Round(2),
					NextProposalData: []byte("value"),
					RoundChangeJustification: []*message.SignedMessage{
						{
							Signature: nil,
							Signers:   []message.OperatorID{1, 2, 3},
							Message: &message.ConsensusMessage{
								MsgType:    message.PrepareMsgType,
								Height:     0,
								Round:      2,
								Identifier: []byte("lambdas"),
								Data:       []byte("value"),
							},
						},
					},
				}),
			},
			expectedError: "change round could not verify signature: failed to verify signature",
		},
	}

	threshold.Init()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// sign if needed
			roundChangeData, err := test.msg.GetRoundChangeData()
			require.NoError(t, err)
			if roundChangeData != nil {
				data, _ := test.msg.GetRoundChangeData()
				test.msg.Data = changeRoundDataToBytes(data)
			}

			signature, err := test.msg.Sign(secretKeys[test.signerID], forksprotocol.V1ForkVersion.String())
			require.NoError(t, err)

			err = changeround2.Validate(instance.ValidatorShare, forksprotocol.V1ForkVersion.String()).
				Run(&message.SignedMessage{
					Signature: signature.Serialize(),
					Signers:   []message.OperatorID{message.OperatorID(test.signerID)},
					Message:   test.msg,
				})
			if len(test.expectedError) > 0 {
				require.EqualError(t, err, test.expectedError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TODO(nkryuchkov): fix this test
func TestRoundChangeJustification(t *testing.T) {
	sks, _ := GenerateNodes(4)

	inputValue := changeRoundDataToBytes(&message.RoundChangeData{
		PreparedValue:            []byte("hello"),
		Round:                    1,
		NextProposalData:         nil,
		RoundChangeJustification: nil,
	})

	round := atomic.Value{}
	round.Store(message.Round(1))

	instance := &Instance{
		ChangeRoundMessages: msgcontinmem.New(3, 2),
		Config:              qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{Committee: map[message.OperatorID]*beacon.Node{
			0: {IbftID: 0},
			1: {IbftID: 1},
			2: {IbftID: 2},
			3: {IbftID: 3},
		}},
		state: &qbft.State{
			Round: round,
		},
	}

	t.Run("no previous prepared", func(t *testing.T) {
		// test no previous prepared round and no round change quorum
		err := instance.JustifyRoundChange(2)
		require.NoError(t, err)
	})

	t.Run("change round quorum no previous prepare", func(t *testing.T) {
		msg := &message.ConsensusMessage{
			MsgType:    message.RoundChangeMsgType,
			Height:     1,
			Round:      2,
			Identifier: []byte("Lambda"),
			Data: changeRoundDataToBytes(&message.RoundChangeData{
				PreparedValue: []byte("data"),
			}),
		}

		prepareData, err := msg.GetPrepareData()
		require.NoError(t, err)

		instance.ChangeRoundMessages.AddMessage(SignMsg(t, 1, sks[1], msg), prepareData.Data)
		instance.ChangeRoundMessages.AddMessage(SignMsg(t, 1, sks[2], msg), prepareData.Data)

		instance.ChangeRoundMessages.AddMessage(&message.SignedMessage{
			Signature: nil,
			Signers:   []message.OperatorID{message.OperatorID(1)},
			Message:   msg,
		}, prepareData.Data)
		instance.ChangeRoundMessages.AddMessage(&message.SignedMessage{
			Signature: nil,
			Signers:   []message.OperatorID{message.OperatorID(2)},
			Message:   msg,
		}, prepareData.Data)
		instance.ChangeRoundMessages.AddMessage(&message.SignedMessage{
			Signature: nil,
			Signers:   []message.OperatorID{message.OperatorID(3)},
			Message:   msg,
		}, prepareData.Data)

		// test no previous prepared round with round change quorum (no justification)
		require.NoError(t, instance.JustifyRoundChange(2))
	})

	t.Run("change round quorum not prepared, instance prepared previously", func(t *testing.T) {
		instance.State().PreparedRound.Store(1)
		instance.State().PreparedValue.Store([]byte("hello"))
		err := instance.JustifyRoundChange(2)
		require.EqualError(t, err, "highest prepared doesn't match prepared state")
	})

	t.Run("change round quorum prepared, instance prepared", func(t *testing.T) {
		instance.ChangeRoundMessages = msgcontinmem.New(3, 2)
		msg1 := &message.SignedMessage{
			Signature: nil,
			Signers:   []message.OperatorID{message.OperatorID(1)},
			Message: &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Height:     1,
				Round:      2,
				Identifier: []byte("Lambda"),
				Data:       inputValue,
			}}
		changeRoundData1, err := msg1.Message.GetRoundChangeData()
		require.NoError(t, err)
		instance.ChangeRoundMessages.AddMessage(msg1, changeRoundData1.GetPreparedValue())

		msg2 := &message.SignedMessage{
			Signature: nil,
			Signers:   []message.OperatorID{message.OperatorID(2)},
			Message: &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Height:     1,
				Round:      2,
				Identifier: []byte("Lambda"),
				Data:       inputValue,
			}}
		changeRoundData2, err := msg2.Message.GetRoundChangeData()
		require.NoError(t, err)
		instance.ChangeRoundMessages.AddMessage(msg2, changeRoundData2.GetPreparedValue())

		msg3 := &message.SignedMessage{
			Signature: nil,
			Signers:   []message.OperatorID{message.OperatorID(3)},
			Message: &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Height:     1,
				Round:      1,
				Identifier: []byte("Lambda"),
				Data:       inputValue,
			}}
		changeRoundData3, err := msg3.Message.GetRoundChangeData()
		require.NoError(t, err)
		instance.ChangeRoundMessages.AddMessage(msg3, changeRoundData3.GetPreparedValue())

		// test no previous prepared round with round change quorum (with justification)
		require.NoError(t, instance.JustifyRoundChange(2))
	})
}

// TODO(nkryuchkov): fix this test
func TestHighestPrepared(t *testing.T) {
	inputValue := []byte("input value")

	instance := &Instance{
		ChangeRoundMessages: msgcontinmem.New(3, 2),
		Config:              qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{Committee: map[message.OperatorID]*beacon.Node{
			0: {IbftID: 0},
			1: {IbftID: 1},
			2: {IbftID: 2},
			3: {IbftID: 3},
		}},
	}

	msg := &message.ConsensusMessage{
		MsgType:    message.RoundChangeMsgType,
		Height:     1,
		Round:      3,
		Identifier: []byte("Lambda"),
		Data:       changeRoundDataToBytes(&message.RoundChangeData{Round: 1, PreparedValue: inputValue}),
	}

	roundChangeData, err := msg.GetRoundChangeData()
	require.NoError(t, err)

	instance.ChangeRoundMessages.AddMessage(&message.SignedMessage{
		Signature: nil,
		Signers:   []message.OperatorID{message.OperatorID(1)},
		Message:   msg,
	}, roundChangeData.PreparedValue)
	instance.ChangeRoundMessages.AddMessage(&message.SignedMessage{
		Signature: nil,
		Signers:   []message.OperatorID{message.OperatorID(2)},
		Message:   msg,
	}, roundChangeData.PreparedValue)

	// test one higher than other
	notPrepared, highest, err := instance.HighestPrepared(3)
	require.NoError(t, err)
	require.False(t, notPrepared)
	require.EqualValues(t, message.Round(2), highest.Round)
	require.EqualValues(t, append(inputValue, []byte("highest")...), highest.PreparedValue)

	// test 2 equals
	instance.ChangeRoundMessages.AddMessage(&message.SignedMessage{
		Signature: nil,
		Signers:   []message.OperatorID{message.OperatorID(2)},
		Message:   msg,
	}, roundChangeData.PreparedValue)

	notPrepared, highest, err = instance.HighestPrepared(3)
	require.NoError(t, err)
	require.False(t, notPrepared)
	require.EqualValues(t, message.Round(2), highest.Round)
	require.EqualValues(t, append(inputValue, []byte("highest")...), highest.PreparedValue)
}

// TODO(nkryuchkov): fix this test
func TestChangeRoundMsgValidationPipeline(t *testing.T) {
	sks, nodes := GenerateNodes(4)

	tests := []struct {
		name          string
		msg           *message.SignedMessage
		expectedError string
	}{
		{
			"valid",
			SignMsg(t, 1, sks[1], &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Height:     1,
				Round:      1,
				Identifier: []byte("Lambda"),
				Data:       []byte("value"), /*changeRoundDataToBytes(&proto.ChangeRoundData{PreparedValue: nil,})*/
			}),
			"",
		},
		{
			"invalid change round data",
			SignMsg(t, 1, sks[1], &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Height:     1,
				Round:      1,
				Identifier: []byte("Lambda"),
				Data:       []byte("value"), /*changeRoundDataToBytes(&proto.ChangeRoundData{PreparedValue: []byte("ad")],})*/
			}),
			"change round justification msg is nil",
		},
		{
			"invalid seq number",
			SignMsg(t, 1, sks[1], &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Height:     2,
				Round:      1,
				Identifier: []byte("Lambda"),
				Data:       []byte("value"), /*changeRoundDataToBytes(&proto.ChangeRoundData{PreparedValue: nil,})*/
			}),
			"invalid message sequence number: expected: 1, actual: 2",
		},

		{
			"invalid lambda",
			SignMsg(t, 1, sks[1], &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Height:     1,
				Round:      1,
				Identifier: []byte("Lambda"),
				Data:       []byte("value"), /*changeRoundDataToBytes(&proto.ChangeRoundData{PreparedValue: nil,})*/
			}),
			"message Lambda (lambdaa) does not equal expected Lambda (lambda)",
		},
		{
			"valid with different round",
			SignMsg(t, 1, sks[1], &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Height:     1,
				Round:      4,
				Identifier: []byte("Lambda"),
				Data:       []byte("value"), /*changeRoundDataToBytes(&proto.ChangeRoundData{PreparedValue: nil,})*/
			}),
			"",
		},
		{
			"invalid msg type",
			SignMsg(t, 1, sks[1], &message.ConsensusMessage{
				MsgType:    message.RoundChangeMsgType,
				Height:     1,
				Round:      1,
				Identifier: []byte("Lambda"),
				Data:       []byte("value"), /*changeRoundDataToBytes(&proto.ChangeRoundData{PreparedValue: nil,})*/
			}),
			"message type is wrong",
		},
	}

	round := atomic.Value{}
	round.Store(message.Round(1))

	height := atomic.Value{}
	height.Store(message.Height(1))

	identifier := atomic.Value{}
	identifier.Store([]byte("lambda"))

	instance := &Instance{
		Config: qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{
			Committee: nodes,
			PublicKey: sks[1].GetPublicKey(), // just placeholder
		},
		state: &qbft.State{
			Round:      round,
			Height:     height,
			Identifier: identifier,
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

	round := atomic.Value{}
	round.Store(message.Round(1))

	height := atomic.Value{}
	height.Store(message.Height(1))

	instance := &Instance{
		PrepareMessages: msgcontinmem.New(3, 2),
		Config:          qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{
			Committee: nodes,
			PublicKey: sks[1].GetPublicKey(), // just placeholder
		},
		state: &qbft.State{
			Round:  round,
			Height: height,
		},
	}
	pipeline := instance.changeRoundFullQuorumMsgPipeline()
	require.EqualValues(t, "if first pipeline non error, continue to second", pipeline.Name())
}

// TODO(nkryuchkov): fix this test
func TestChangeRoundPipeline(t *testing.T) {
	sks, nodes := GenerateNodes(4)

	round := atomic.Value{}
	round.Store(message.Round(1))

	height := atomic.Value{}
	height.Store(message.Height(1))

	instance := &Instance{
		PrepareMessages: msgcontinmem.New(3, 2),
		Config:          qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{
			Committee: nodes,
			PublicKey: sks[1].GetPublicKey(), // just placeholder
		},
		state: &qbft.State{
			Round:  round,
			Height: height,
		},
	}
	instance.fork = testingFork(instance)
	pipeline := instance.ChangeRoundMsgPipeline()
	require.EqualValues(t, "combination of: combination of: basic msg validation, type check, lambda, sequence, authorize, validateJustification msg, , add change round msg, upon change round partial quorum, if first pipeline non error, continue to second, ", pipeline.Name())
}

func prepareDataToBytes(input *message.PrepareData) []byte {
	ret, _ := json.Marshal(input)
	return ret
}
