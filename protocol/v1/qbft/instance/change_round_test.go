package instance

import (
	"encoding/json"
	"github.com/bloxapp/ssv/protocol/v1/types"
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont"
	msgcontinmem "github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont/inmem"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/changeround"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/preprepare"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/signedmsg"
	protocoltesting "github.com/bloxapp/ssv/protocol/v1/testing"
	"github.com/bloxapp/ssv/utils/threshold"
)

type testFork struct {
	instance *Instance
}

func (v0 *testFork) VersionName() string {
	return "v0"
}

// TODO: (lint) fix test
//nolint
func (v0 *testFork) Apply(instance Instance) {

}

// PrePrepareMsgPipelineV0 is the full processing msg pipeline for a pre-prepare msg
func (v0 *testFork) PrePrepareMsgValidationPipeline(share *beacon.Share, state *qbft.State, roundLeader preprepare.LeaderResolver) pipelines.SignedMessagePipeline {
	identifier := state.GetIdentifier()
	return pipelines.Combine(
		signedmsg.BasicMsgValidation(),
		signedmsg.MsgTypeCheck(specqbft.ProposalMsgType),
		signedmsg.ValidateLambdas(identifier[:]),
		signedmsg.ValidateSequenceNumber(state.GetHeight()),
		signedmsg.AuthorizeMsg(share),
		preprepare.ValidatePrePrepareMsg(roundLeader),
	)
}

// PrepareMsgPipeline is the full processing msg pipeline for a prepare msg
func (v0 *testFork) PrepareMsgValidationPipeline(share *beacon.Share, state *qbft.State) pipelines.SignedMessagePipeline {
	identifier := state.GetIdentifier()
	return pipelines.Combine(
		signedmsg.BasicMsgValidation(),
		signedmsg.MsgTypeCheck(specqbft.PrepareMsgType),
		signedmsg.ValidateLambdas(identifier[:]),
		signedmsg.ValidateSequenceNumber(state.GetHeight()),
		signedmsg.AuthorizeMsg(share),
	)
}

// CommitMsgValidationPipeline is a msg validation ONLY pipeline
func (v0 *testFork) CommitMsgValidationPipeline(share *beacon.Share, identifier []byte, height specqbft.Height) pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		signedmsg.BasicMsgValidation(),
		signedmsg.MsgTypeCheck(specqbft.CommitMsgType),
		signedmsg.ValidateLambdas(identifier[:]),
		signedmsg.ValidateSequenceNumber(height),
		signedmsg.AuthorizeMsg(share),
	)
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
func (v0 *testFork) ChangeRoundMsgValidationPipeline(share *beacon.Share, identifier []byte, height specqbft.Height) pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		signedmsg.BasicMsgValidation(),
		signedmsg.MsgTypeCheck(specqbft.RoundChangeMsgType),
		signedmsg.ValidateLambdas(identifier[:]),
		signedmsg.ValidateSequenceNumber(height),
		signedmsg.AuthorizeMsg(share),
		changeround.Validate(share),
	)
}

// ChangeRoundMsgPipeline is the full processing msg pipeline for a change round msg
func (v0 *testFork) ChangeRoundMsgPipeline() pipelines.SignedMessagePipeline {
	return v0.instance.ChangeRoundMsgPipeline()
}

func testingFork(instance *Instance) *testFork {
	return &testFork{instance: instance}
}

func changeRoundDataToBytes(t *testing.T, input *specqbft.RoundChangeData) []byte {
	ret, err := json.Marshal(input)
	require.NoError(t, err)
	return ret
}
func bytesToChangeRoundData(input []byte) *specqbft.RoundChangeData {
	ret := &specqbft.RoundChangeData{}
	json.Unmarshal(input, ret)
	return ret
}

// GenerateNodes generates randomly nodes
func GenerateNodes(cnt int) (
	map[spectypes.OperatorID]*bls.SecretKey,
	map[spectypes.OperatorID]*beacon.Node,
	[]spectypes.OperatorID,
	[]uint64,
) {
	_ = bls.Init(bls.BLS12_381)
	nodes := make(map[spectypes.OperatorID]*beacon.Node)
	sks := make(map[spectypes.OperatorID]*bls.SecretKey)
	operatorIds := []spectypes.OperatorID{78, 12, 99, 1}
	shareOperatorIds := make([]uint64, len(operatorIds))
	for i := 0; i < cnt; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()

		nodes[operatorIds[i]] = &beacon.Node{
			IbftID: uint64(operatorIds[i]),
			Pk:     sk.GetPublicKey().Serialize(),
		}
		sks[operatorIds[i]] = sk
		shareOperatorIds[i] = uint64(operatorIds[i])
	}
	return sks, nodes, operatorIds, shareOperatorIds
}

// SignMsg signs the given message by the given private key
func SignMsg(t *testing.T, signers []spectypes.OperatorID, sk *bls.SecretKey, msg *specqbft.Message) *specqbft.SignedMessage {
	sigType := spectypes.QBFTSignatureType
	domain := spectypes.ComputeSignatureDomain(types.GetDefaultDomain(), sigType)
	sigRoot, err := spectypes.ComputeSigningRoot(msg, domain)
	require.NoError(t, err)
	sig := sk.SignByte(sigRoot)

	return &specqbft.SignedMessage{
		Message:   msg,
		Signers:   signers,
		Signature: sig.Serialize(),
	}
}

func TestRoundChangeInputValue(t *testing.T) {
	secretKey, nodes, operatorIds, shareOperatorIds := GenerateNodes(4)
	round := atomic.Value{}
	round.Store(specqbft.Round(1))

	instance := &Instance{
		Logger: zap.L(),
		containersMap: map[specqbft.MessageType]msgcont.MessageContainer{
			specqbft.PrepareMsgType: msgcontinmem.New(3, 2),
		},
		Config:         qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{Committee: nodes, OperatorIds: shareOperatorIds},
		state: &qbft.State{
			Round: round,
		},
	}

	// no prepared round
	byts, err := instance.roundChangeInputValue()
	require.NoError(t, err)
	require.NotNil(t, byts)
	noPrepareChangeRoundData := specqbft.RoundChangeData{}
	require.NoError(t, json.Unmarshal(byts, &noPrepareChangeRoundData))
	require.Nil(t, noPrepareChangeRoundData.PreparedValue)
	require.EqualValues(t, uint64(0), noPrepareChangeRoundData.PreparedRound)
	require.Nil(t, noPrepareChangeRoundData.RoundChangeJustification)

	// add votes
	msg := &specqbft.Message{
		MsgType:    specqbft.PrepareMsgType,
		Height:     1,
		Round:      1,
		Identifier: []byte("Lambda"),
		Data:       prepareDataToBytes(t, &specqbft.PrepareData{Data: []byte("value")}),
	}

	prepareData, err := msg.GetPrepareData()
	require.NoError(t, err)

	instance.containersMap[specqbft.PrepareMsgType].AddMessage(SignMsg(t, operatorIds[:1], secretKey[operatorIds[0]], msg), prepareData.Data)
	instance.containersMap[specqbft.PrepareMsgType].AddMessage(SignMsg(t, operatorIds[1:2], secretKey[operatorIds[1]], msg), prepareData.Data)

	// with some prepare votes but not enough
	byts, err = instance.roundChangeInputValue()
	require.NoError(t, err)
	require.NotNil(t, byts)
	noPrepareChangeRoundData = specqbft.RoundChangeData{}
	require.NoError(t, json.Unmarshal(byts, &noPrepareChangeRoundData))
	require.Nil(t, noPrepareChangeRoundData.PreparedValue)
	require.EqualValues(t, uint64(0), noPrepareChangeRoundData.PreparedRound)
	require.Nil(t, noPrepareChangeRoundData.RoundChangeJustification)

	// add more votes
	instance.containersMap[specqbft.PrepareMsgType].AddMessage(SignMsg(t, operatorIds[2:3], secretKey[operatorIds[2]], msg), prepareData.Data)
	instance.State().PreparedRound.Store(specqbft.Round(1))
	instance.State().PreparedValue.Store([]byte("value"))

	// with a prepared round
	byts, err = instance.roundChangeInputValue()
	require.NoError(t, err)
	require.NotNil(t, byts)
	data := bytesToChangeRoundData(byts)
	require.EqualValues(t, 1, data.PreparedRound)
	require.EqualValues(t, []byte("value"), data.PreparedValue)
}

func TestValidateChangeRoundMessage(t *testing.T) {
	secretKeys, nodes, operatorIds, shareOperatorIds := GenerateNodes(4)
	round := atomic.Value{}
	round.Store(specqbft.Round(1))

	instance := &Instance{
		Config:         qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{Committee: nodes, OperatorIds: shareOperatorIds},
		state: &qbft.State{
			Round: round,
		},
	}

	consensusMessage := &specqbft.Message{
		MsgType:    specqbft.PrepareMsgType,
		Height:     0,
		Round:      2,
		Identifier: []byte("Lambdas"),
		Data:       prepareDataToBytes(t, &specqbft.PrepareData{Data: []byte("value")}),
	}

	twoSigners := map[spectypes.OperatorID]*bls.SecretKey{
		operatorIds[0]: secretKeys[operatorIds[0]],
		operatorIds[1]: secretKeys[operatorIds[1]],
	}

	consensusMessageSignatureByTwo := aggregateSign(t, twoSigners, consensusMessage)

	tests := []struct {
		name                string
		msg                 *specqbft.Message
		signerID            spectypes.OperatorID
		justificationSigIds []uint64
		expectedError       string
	}{
		{
			name:     "valid 1",
			signerID: operatorIds[0],
			msg: &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Round:      1,
				Identifier: []byte("Lambda"),
				Data:       changeRoundDataToBytes(t, &specqbft.RoundChangeData{}),
			},
			expectedError: "",
		},
		{
			name:     "valid 2",
			signerID: operatorIds[0],
			msg: &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Round:      2,
				Identifier: []byte("Lambda"),
				Data:       changeRoundDataToBytes(t, &specqbft.RoundChangeData{}),
			},
			expectedError: "",
		},
		{
			name:     "valid 3",
			signerID: operatorIds[0],
			msg: &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Round:      3,
				Identifier: []byte("Lambda"),
				Data:       changeRoundDataToBytes(t, &specqbft.RoundChangeData{}),
			},
			expectedError: "",
		},
		{
			name:     "valid 4",
			signerID: operatorIds[0],
			msg: &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Round:      3,
				Identifier: []byte("Lambda"),
				Data:       changeRoundDataToBytes(t, &specqbft.RoundChangeData{}),
			},
			expectedError: "",
		},
		{
			name:                "valid justification 1",
			signerID:            operatorIds[0],
			justificationSigIds: shareOperatorIds[:3],
			msg: &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Round:      3,
				Identifier: []byte("Lambdas"),
				Data: changeRoundDataToBytes(t, &specqbft.RoundChangeData{
					PreparedValue: []byte("value"),
					PreparedRound: specqbft.Round(2),
					RoundChangeJustification: []*specqbft.SignedMessage{
						protocoltesting.SignMsg(t, secretKeys, []spectypes.OperatorID{operatorIds[0]}, consensusMessage),
						protocoltesting.SignMsg(t, secretKeys, []spectypes.OperatorID{operatorIds[1]}, consensusMessage),
						protocoltesting.SignMsg(t, secretKeys, []spectypes.OperatorID{operatorIds[2]}, consensusMessage),
					},
				}),
			},
			expectedError: "",
		},
		{
			name:                "invalid justification msg type",
			signerID:            operatorIds[0],
			justificationSigIds: shareOperatorIds[:3],
			msg: &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Round:      3,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(t, &specqbft.RoundChangeData{
					PreparedValue: []byte("value"),
					PreparedRound: specqbft.Round(2),
					RoundChangeJustification: []*specqbft.SignedMessage{
						{
							Signature: nil,
							Signers:   operatorIds[:3],
							Message: &specqbft.Message{
								MsgType:    specqbft.ProposalMsgType,
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
			signerID:            operatorIds[0],
			justificationSigIds: shareOperatorIds[:3],
			msg: &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Round:      3,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(t, &specqbft.RoundChangeData{
					PreparedValue: []byte("value"),
					PreparedRound: specqbft.Round(2),
					RoundChangeJustification: []*specqbft.SignedMessage{
						{
							Signature: nil,
							Signers:   operatorIds[:3],
							Message: &specqbft.Message{
								MsgType:    specqbft.PrepareMsgType,
								Height:     0,
								Round:      3,
								Identifier: []byte("lambdas"),
								Data:       []byte("value"),
							},
						},
					},
				}),
			},
			expectedError: "round change justification invalid: msg round wrong",
		},
		{
			name:                "invalid prepared and justification round",
			signerID:            operatorIds[0],
			justificationSigIds: shareOperatorIds[:3],
			msg: &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Round:      3,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(t, &specqbft.RoundChangeData{
					PreparedValue: []byte("value"),
					PreparedRound: specqbft.Round(2),
					RoundChangeJustification: []*specqbft.SignedMessage{
						{
							Signature: nil,
							Signers:   operatorIds[:3],
							Message: &specqbft.Message{
								MsgType:    specqbft.PrepareMsgType,
								Height:     0,
								Round:      1,
								Identifier: []byte("lambdas"),
								Data:       []byte("value"),
							},
						},
					},
				}),
			},
			expectedError: "round change justification invalid: msg round wrong",
		},
		{
			name:                "invalid justification instance",
			signerID:            operatorIds[0],
			justificationSigIds: shareOperatorIds[:3],
			msg: &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Round:      3,
				Identifier: []byte("Lambda"),
				Data: changeRoundDataToBytes(t, &specqbft.RoundChangeData{
					PreparedValue: []byte("value"),
					PreparedRound: specqbft.Round(2),
					RoundChangeJustification: []*specqbft.SignedMessage{
						{
							Signature: nil,
							Signers:   operatorIds[:3],
							Message: &specqbft.Message{
								MsgType:    specqbft.PrepareMsgType,
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
			signerID:            operatorIds[0],
			justificationSigIds: shareOperatorIds[:2],
			msg: &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Round:      3,
				Identifier: []byte("lambdas"),
				Data: changeRoundDataToBytes(t, &specqbft.RoundChangeData{
					PreparedValue: []byte("value"),
					PreparedRound: specqbft.Round(2),
					RoundChangeJustification: []*specqbft.SignedMessage{
						protocoltesting.SignMsg(t, secretKeys, []spectypes.OperatorID{operatorIds[0]}, &specqbft.Message{
							MsgType:    specqbft.PrepareMsgType,
							Height:     0,
							Round:      2,
							Identifier: []byte("lambdas"),
							Data:       prepareDataToBytes(t, &specqbft.PrepareData{Data: []byte("value")}),
						}),
						protocoltesting.SignMsg(t, secretKeys, []spectypes.OperatorID{operatorIds[1]}, &specqbft.Message{
							MsgType:    specqbft.PrepareMsgType,
							Height:     0,
							Round:      2,
							Identifier: []byte("lambdas"),
							Data:       prepareDataToBytes(t, &specqbft.PrepareData{Data: []byte("value")}),
						}),
					},
				}),
			},
			expectedError: "no justifications quorum",
		},
		{
			name:                "valid justification 2",
			signerID:            operatorIds[0],
			justificationSigIds: shareOperatorIds[:3],
			msg: &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Round:      3,
				Identifier: []byte("lambdas"),
				Data: changeRoundDataToBytes(t, &specqbft.RoundChangeData{
					PreparedValue: []byte("value"),
					PreparedRound: specqbft.Round(2),
					RoundChangeJustification: []*specqbft.SignedMessage{
						{
							Signature: nil,
							Signers:   operatorIds[:3],
							Message: &specqbft.Message{
								MsgType:    specqbft.PrepareMsgType,
								Height:     0,
								Round:      2,
								Identifier: []byte("lambdas"),
								Data:       prepareDataToBytes(t, &specqbft.PrepareData{Data: []byte("values")}),
							},
						},
					},
				}),
			},
			expectedError: "round change justification invalid: prepare data != proposed data",
		},
		{
			name:                "invalid justification sig",
			signerID:            operatorIds[0],
			justificationSigIds: shareOperatorIds[:2],
			msg: &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Round:      3,
				Identifier: []byte("Lambdas"),
				Data: changeRoundDataToBytes(t, &specqbft.RoundChangeData{
					PreparedValue: []byte("value"),
					PreparedRound: specqbft.Round(2),
					RoundChangeJustification: []*specqbft.SignedMessage{
						{
							Signature: consensusMessageSignatureByTwo.Serialize(),
							Signers:   operatorIds[0:1],
							Message:   consensusMessage,
						},
						{
							Signature: consensusMessageSignatureByTwo.Serialize(),
							Signers:   operatorIds[1:2],
							Message:   consensusMessage,
						},
						{
							Signature: consensusMessageSignatureByTwo.Serialize(),
							Signers:   operatorIds[2:3],
							Message:   consensusMessage,
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
				test.msg.Data = changeRoundDataToBytes(t, data)
			}

			signature, err := signMessage(test.msg, secretKeys[test.signerID])
			require.NoError(t, err)

			err = changeround.Validate(instance.ValidatorShare).
				Run(&specqbft.SignedMessage{
					Signature: signature.Serialize(),
					Signers:   []spectypes.OperatorID{test.signerID},
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

func TestRoundChangeJustification(t *testing.T) {
	sks, nodes, operatorIds, shareOperatorIds := GenerateNodes(4)

	inputValue := changeRoundDataToBytes(t, &specqbft.RoundChangeData{
		PreparedValue:            []byte("hello"),
		PreparedRound:            1,
		RoundChangeJustification: nil,
	})

	round := atomic.Value{}
	round.Store(specqbft.Round(1))

	instance := &Instance{
		containersMap: map[specqbft.MessageType]msgcont.MessageContainer{
			specqbft.RoundChangeMsgType: msgcontinmem.New(3, 2),
		},
		Config:         qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{Committee: nodes, OperatorIds: shareOperatorIds},
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
		msg := &specqbft.Message{
			MsgType:    specqbft.RoundChangeMsgType,
			Height:     1,
			Round:      2,
			Identifier: []byte("Lambda"),
			Data:       changeRoundDataToBytes(t, &specqbft.RoundChangeData{}),
		}

		prepareData, err := msg.GetPrepareData()
		require.NoError(t, err)

		instance.containersMap[specqbft.RoundChangeMsgType].AddMessage(SignMsg(t, operatorIds[:1], sks[operatorIds[0]], msg), prepareData.Data)
		instance.containersMap[specqbft.RoundChangeMsgType].AddMessage(SignMsg(t, operatorIds[:1], sks[operatorIds[1]], msg), prepareData.Data)

		instance.containersMap[specqbft.RoundChangeMsgType].AddMessage(&specqbft.SignedMessage{
			Signature: nil,
			Signers:   operatorIds[:1],
			Message:   msg,
		}, prepareData.Data)
		instance.containersMap[specqbft.RoundChangeMsgType].AddMessage(&specqbft.SignedMessage{
			Signature: nil,
			Signers:   operatorIds[1:2],
			Message:   msg,
		}, prepareData.Data)
		instance.containersMap[specqbft.RoundChangeMsgType].AddMessage(&specqbft.SignedMessage{
			Signature: nil,
			Signers:   operatorIds[2:3],
			Message:   msg,
		}, prepareData.Data)

		// test no previous prepared round with round change quorum (no justification)
		require.NoError(t, instance.JustifyRoundChange(2))
	})

	t.Run("change round quorum not prepared, instance prepared previously", func(t *testing.T) {
		instance.State().PreparedRound.Store(specqbft.Round(1))
		instance.State().PreparedValue.Store([]byte("hello"))
		err := instance.JustifyRoundChange(2)
		require.EqualError(t, err, "highest prepared doesn't match prepared state")
	})

	t.Run("change round quorum prepared, instance prepared", func(t *testing.T) {
		instance.containersMap[specqbft.RoundChangeMsgType] = msgcontinmem.New(3, 2)
		msg1 := &specqbft.SignedMessage{
			Signature: nil,
			Signers:   operatorIds[:1],
			Message: &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Height:     1,
				Round:      2,
				Identifier: []byte("Lambda"),
				Data:       inputValue,
			}}
		changeRoundData1, err := msg1.Message.GetRoundChangeData()
		require.NoError(t, err)
		instance.containersMap[specqbft.RoundChangeMsgType].AddMessage(msg1, changeRoundData1.PreparedValue)

		msg2 := &specqbft.SignedMessage{
			Signature: nil,
			Signers:   operatorIds[1:2],
			Message: &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Height:     1,
				Round:      2,
				Identifier: []byte("Lambda"),
				Data:       inputValue,
			}}
		changeRoundData2, err := msg2.Message.GetRoundChangeData()
		require.NoError(t, err)
		instance.containersMap[specqbft.RoundChangeMsgType].AddMessage(msg2, changeRoundData2.PreparedValue)

		msg3 := &specqbft.SignedMessage{
			Signature: nil,
			Signers:   operatorIds[2:3],
			Message: &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Height:     1,
				Round:      1,
				Identifier: []byte("Lambda"),
				Data:       inputValue,
			}}
		changeRoundData3, err := msg3.Message.GetRoundChangeData()
		require.NoError(t, err)
		instance.containersMap[specqbft.RoundChangeMsgType].AddMessage(msg3, changeRoundData3.PreparedValue)

		// test no previous prepared round with round change quorum (with justification)
		require.NoError(t, instance.JustifyRoundChange(2))
	})
}

func TestHighestPrepared(t *testing.T) {
	inputValue := []byte("input value")

	instance := &Instance{
		containersMap: map[specqbft.MessageType]msgcont.MessageContainer{
			specqbft.RoundChangeMsgType: msgcontinmem.New(3, 2),
		},
		Config: qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{Committee: map[spectypes.OperatorID]*beacon.Node{
			0: {IbftID: 0},
			1: {IbftID: 1},
			2: {IbftID: 2},
			3: {IbftID: 3},
		}},
	}

	msg1 := &specqbft.Message{
		MsgType:    specqbft.RoundChangeMsgType,
		Height:     1,
		Round:      3,
		Identifier: []byte("Lambda"),
		Data:       changeRoundDataToBytes(t, &specqbft.RoundChangeData{PreparedRound: 1, PreparedValue: inputValue}),
	}

	msg2 := &specqbft.Message{
		MsgType:    specqbft.RoundChangeMsgType,
		Height:     1,
		Round:      3,
		Identifier: []byte("Lambda"),
		Data:       changeRoundDataToBytes(t, &specqbft.RoundChangeData{PreparedRound: 2, PreparedValue: append(inputValue, []byte("highest")...)}),
	}

	roundChangeData, err := msg1.GetRoundChangeData()
	require.NoError(t, err)

	instance.containersMap[specqbft.RoundChangeMsgType].AddMessage(&specqbft.SignedMessage{
		Signature: nil,
		Signers:   []spectypes.OperatorID{1},
		Message:   msg1,
	}, roundChangeData.PreparedValue)
	instance.containersMap[specqbft.RoundChangeMsgType].AddMessage(&specqbft.SignedMessage{
		Signature: nil,
		Signers:   []spectypes.OperatorID{2},
		Message:   msg2,
	}, roundChangeData.PreparedValue)

	// test one higher than other
	notPrepared, highest, err := instance.HighestPrepared(3)
	require.NoError(t, err)
	require.False(t, notPrepared)
	require.EqualValues(t, specqbft.Round(2), highest.PreparedRound)
	require.EqualValues(t, append(inputValue, []byte("highest")...), highest.PreparedValue)

	// test 2 equals
	instance.containersMap[specqbft.RoundChangeMsgType].AddMessage(&specqbft.SignedMessage{
		Signature: nil,
		Signers:   []spectypes.OperatorID{2},
		Message:   msg2,
	}, roundChangeData.PreparedValue)

	notPrepared, highest, err = instance.HighestPrepared(3)
	require.NoError(t, err)
	require.False(t, notPrepared)
	require.EqualValues(t, specqbft.Round(2), highest.PreparedRound)
	require.EqualValues(t, append(inputValue, []byte("highest")...), highest.PreparedValue)
}

func TestChangeRoundMsgValidationPipeline(t *testing.T) {
	sks, nodes, operatorIds, shareOperatorIds := GenerateNodes(4)
	msgID := spectypes.NewMsgID([]byte("Lambda"), spectypes.BNRoleAttester)
	tests := []struct {
		name          string
		msg           *specqbft.SignedMessage
		expectedError string
	}{
		{
			"valid",
			SignMsg(t, operatorIds[:1], sks[operatorIds[0]], &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Height:     1,
				Round:      1,
				Identifier: msgID[:],
				Data:       changeRoundDataToBytes(t, &specqbft.RoundChangeData{PreparedValue: nil}),
			}),
			"",
		},
		{
			"invalid change round data",
			SignMsg(t, operatorIds[:1], sks[operatorIds[0]], &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Height:     1,
				Round:      1,
				Identifier: msgID[:],
				Data:       changeRoundDataToBytes(t, &specqbft.RoundChangeData{PreparedValue: []byte("ad")}),
			}),
			"roundChangeData invalid: round change justification invalid",
		},
		{
			"invalid seq number",
			SignMsg(t, operatorIds[:1], sks[operatorIds[0]], &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Height:     2,
				Round:      1,
				Identifier: msgID[:],
				Data:       changeRoundDataToBytes(t, &specqbft.RoundChangeData{PreparedValue: nil}),
			}),
			"msg Height wrong",
		},

		{
			"invalid lambda",
			SignMsg(t, operatorIds[:1], sks[operatorIds[0]], &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Height:     1,
				Round:      1,
				Identifier: []byte("lambdaa"),
				Data:       changeRoundDataToBytes(t, &specqbft.RoundChangeData{PreparedValue: nil}),
			}),
			"message Lambda (6c616d62646161) does not equal expected Lambda (4c616d62646100000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000)",
		},
		{
			"valid with different round",
			SignMsg(t, operatorIds[:1], sks[operatorIds[0]], &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Height:     1,
				Round:      4,
				Identifier: msgID[:],
				Data:       changeRoundDataToBytes(t, &specqbft.RoundChangeData{PreparedValue: nil}),
			}),
			"",
		},
		{
			"invalid msg type",
			SignMsg(t, operatorIds[:1], sks[operatorIds[0]], &specqbft.Message{
				MsgType:    specqbft.CommitMsgType,
				Height:     1,
				Round:      1,
				Identifier: msgID[:],
				Data:       changeRoundDataToBytes(t, &specqbft.RoundChangeData{PreparedValue: nil}),
			}),
			"message type is wrong",
		},
	}

	round := atomic.Value{}
	round.Store(specqbft.Round(1))

	height := atomic.Value{}
	height.Store(specqbft.Height(1))

	identifier := atomic.Value{}
	identifier.Store(msgID[:])

	instance := &Instance{
		Config: qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{
			Committee:   nodes,
			PublicKey:   sks[operatorIds[0]].GetPublicKey(), // just placeholder
			OperatorIds: shareOperatorIds,
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
	sks, nodes, operatorIds, shareOperatorIds := GenerateNodes(4)

	round := atomic.Value{}
	round.Store(specqbft.Round(1))

	height := atomic.Value{}
	height.Store(specqbft.Height(1))

	instance := &Instance{
		containersMap: map[specqbft.MessageType]msgcont.MessageContainer{
			specqbft.PrepareMsgType: msgcontinmem.New(3, 2),
		},
		Config: qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{
			Committee:   nodes,
			PublicKey:   sks[operatorIds[0]].GetPublicKey(), // just placeholder
			OperatorIds: shareOperatorIds,
		},
		state: &qbft.State{
			Round:  round,
			Height: height,
		},
	}
	pipeline := instance.changeRoundFullQuorumMsgPipeline()
	require.EqualValues(t, "if first pipeline non error, continue to second", pipeline.Name())
}

func TestChangeRoundPipeline(t *testing.T) {
	sks, nodes, operatorIds, shareOperatorIds := GenerateNodes(4)

	round := atomic.Value{}
	round.Store(specqbft.Round(1))

	height := atomic.Value{}
	height.Store(specqbft.Height(1))

	instance := &Instance{
		containersMap: map[specqbft.MessageType]msgcont.MessageContainer{
			specqbft.PrepareMsgType: msgcontinmem.New(3, 2),
		},
		Config: qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{
			Committee:   nodes,
			PublicKey:   sks[operatorIds[0]].GetPublicKey(), // just placeholder
			OperatorIds: shareOperatorIds,
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

func prepareDataToBytes(t *testing.T, input *specqbft.PrepareData) []byte {
	ret, err := input.Encode()
	require.NoError(t, err)
	return ret
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

func signMessage(msg *specqbft.Message, sk *bls.SecretKey) (*bls.Sign, error) {
	signatureDomain := spectypes.ComputeSignatureDomain(types.GetDefaultDomain(), spectypes.QBFTSignatureType)
	root, err := spectypes.ComputeSigningRoot(msg, signatureDomain)
	if err != nil {
		return nil, err
	}
	return sk.SignByte(root), nil
}
