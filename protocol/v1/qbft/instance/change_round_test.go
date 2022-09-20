package instance

import (
	"encoding/json"
	"github.com/bloxapp/ssv/utils/logex"
	"go.uber.org/zap/zapcore"
	"testing"
	"time"

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
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/proposal"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/signedmsg"
	protocoltesting "github.com/bloxapp/ssv/protocol/v1/testing"
	"github.com/bloxapp/ssv/protocol/v1/types"
	"github.com/bloxapp/ssv/utils/threshold"
)

func TestHighestRoundTimeoutSeconds(t *testing.T) {
	round := atomic.Value{}
	round.Store(specqbft.Round(6))
	instance := &Instance{
		Logger: zap.L(),
		Config: qbft.DefaultConsensusParams(),
		State: &qbft.State{
			Round: round,
		},
	}

	require.Equal(t, time.Duration(0), instance.HighestRoundTimeoutSeconds())
	instance.GetState().Round.Store(specqbft.Round(7))
	require.Equal(t, time.Second*81, instance.HighestRoundTimeoutSeconds())
	round.Store(specqbft.Round(10))
	instance.GetState().Round.Store(specqbft.Round(10))
	require.Equal(t, time.Second*(27+(60*36)), instance.HighestRoundTimeoutSeconds())
}

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

// ProposalMsgValidationPipeline is the full processing msg pipeline for a proposal msg
func (v0 *testFork) ProposalMsgValidationPipeline(share *beacon.Share, state *qbft.State, roundLeader proposal.LeaderResolver) pipelines.SignedMessagePipeline {
	identifier := state.GetIdentifier()
	return pipelines.Combine(
		signedmsg.BasicMsgValidation(),
		signedmsg.MsgTypeCheck(specqbft.ProposalMsgType),
		signedmsg.ValidateSequenceNumber(state.GetHeight()),
		signedmsg.ValidateIdentifiers(identifier[:]),
		signedmsg.AuthorizeMsg(share),
		proposal.ValidateProposalMsg(share, state, roundLeader),
	)
}

// PrepareMsgPipeline is the full processing msg pipeline for a prepare msg
func (v0 *testFork) PrepareMsgValidationPipeline(share *beacon.Share, state *qbft.State) pipelines.SignedMessagePipeline {
	identifier := state.GetIdentifier()
	return pipelines.Combine(
		signedmsg.BasicMsgValidation(),
		signedmsg.MsgTypeCheck(specqbft.PrepareMsgType),
		signedmsg.ValidateIdentifiers(identifier[:]),
		signedmsg.ValidateSequenceNumber(state.GetHeight()),
		signedmsg.AuthorizeMsg(share),
	)
}

// CommitMsgValidationPipeline is a msg validation ONLY pipeline
func (v0 *testFork) CommitMsgValidationPipeline(share *beacon.Share, state *qbft.State) pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		signedmsg.BasicMsgValidation(),
		signedmsg.MsgTypeCheck(specqbft.CommitMsgType),
		signedmsg.ValidateIdentifiers(state.GetIdentifier()),
		signedmsg.ValidateSequenceNumber(state.GetHeight()),
		signedmsg.AuthorizeMsg(share),
	)
}

// CommitMsgPipeline is the full processing msg pipeline for a commit msg
func (v0 *testFork) CommitMsgPipeline() pipelines.SignedMessagePipeline {
	return v0.instance.CommitMsgPipeline()
}

// changeRoundMsgValidationPipeline is a msg validation ONLY pipeline for a change round msg
func (v0 *testFork) ChangeRoundMsgValidationPipeline(share *beacon.Share, state *qbft.State) pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		signedmsg.BasicMsgValidation(),
		signedmsg.MsgTypeCheck(specqbft.RoundChangeMsgType),
		signedmsg.ValidateIdentifiers(state.GetIdentifier()),
		signedmsg.ValidateSequenceNumber(state.GetHeight()),
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
		ContainersMap: map[specqbft.MessageType]msgcont.MessageContainer{
			specqbft.PrepareMsgType: msgcontinmem.New(3, 2),
		},
		Config:         qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{Committee: nodes, OperatorIds: shareOperatorIds},
		State: &qbft.State{
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
		Identifier: []byte("Identifier"),
		Data:       prepareDataToBytes(t, &specqbft.PrepareData{Data: []byte("value")}),
	}

	prepareData, err := msg.GetPrepareData()
	require.NoError(t, err)

	instance.ContainersMap[specqbft.PrepareMsgType].AddMessage(SignMsg(t, operatorIds[:1], secretKey[operatorIds[0]], msg), prepareData.Data)
	instance.ContainersMap[specqbft.PrepareMsgType].AddMessage(SignMsg(t, operatorIds[1:2], secretKey[operatorIds[1]], msg), prepareData.Data)

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
	instance.ContainersMap[specqbft.PrepareMsgType].AddMessage(SignMsg(t, operatorIds[2:3], secretKey[operatorIds[2]], msg), prepareData.Data)
	instance.GetState().PreparedRound.Store(specqbft.Round(1))
	instance.GetState().PreparedValue.Store([]byte("value"))

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
		State: &qbft.State{
			Round: round,
		},
	}

	consensusMessage := &specqbft.Message{
		MsgType:    specqbft.PrepareMsgType,
		Height:     0,
		Round:      2,
		Identifier: []byte("Identifiers"),
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
				Identifier: []byte("Identifier"),
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
				Identifier: []byte("Identifier"),
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
				Identifier: []byte("Identifier"),
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
				Identifier: []byte("Identifier"),
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
				Identifier: []byte("Identifiers"),
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
				Identifier: []byte("Identifier"),
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
								Identifier: []byte("identifiers"),
								Data:       []byte("value"),
							},
						},
					},
				}),
			},
			expectedError: "round change justification invalid: change round justification msg type not Prepare (0)",
		},
		{
			name:                "invalid justification round",
			signerID:            operatorIds[0],
			justificationSigIds: shareOperatorIds[:3],
			msg: &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Round:      3,
				Identifier: []byte("Identifier"),
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
								Identifier: []byte("identifiers"),
								Data:       []byte("value"),
							},
						},
					},
				}),
			},
			expectedError: "round change justification invalid: change round justification round lower or equal to message round",
		},
		{
			name:                "invalid prepared and justification round",
			signerID:            operatorIds[0],
			justificationSigIds: shareOperatorIds[:3],
			msg: &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Round:      3,
				Identifier: []byte("Identifier"),
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
								Identifier: []byte("identifiers"),
								Data:       []byte("value"),
							},
						},
					},
				}),
			},
			expectedError: "round change justification invalid: change round prepared round not equal to justification msg round",
		},
		{
			name:                "invalid justification instance",
			signerID:            operatorIds[0],
			justificationSigIds: shareOperatorIds[:3],
			msg: &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Round:      3,
				Identifier: []byte("Identifier"),
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
								Identifier: []byte("identifiers"),
								Data:       []byte("value"),
							},
						},
					},
				}),
			},
			expectedError: "round change justification invalid: change round justification msg identifier not equal to msg identifier not equal to instance identifier",
		},
		{
			name:                "invalid justification quorum",
			signerID:            operatorIds[0],
			justificationSigIds: shareOperatorIds[:2],
			msg: &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Round:      3,
				Identifier: []byte("identifiers"),
				Data: changeRoundDataToBytes(t, &specqbft.RoundChangeData{
					PreparedValue: []byte("value"),
					PreparedRound: specqbft.Round(2),
					RoundChangeJustification: []*specqbft.SignedMessage{
						protocoltesting.SignMsg(t, secretKeys, []spectypes.OperatorID{operatorIds[0]}, &specqbft.Message{
							MsgType:    specqbft.PrepareMsgType,
							Height:     0,
							Round:      2,
							Identifier: []byte("identifiers"),
							Data:       prepareDataToBytes(t, &specqbft.PrepareData{Data: []byte("value")}),
						}),
						protocoltesting.SignMsg(t, secretKeys, []spectypes.OperatorID{operatorIds[1]}, &specqbft.Message{
							MsgType:    specqbft.PrepareMsgType,
							Height:     0,
							Round:      2,
							Identifier: []byte("identifiers"),
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
				Identifier: []byte("identifiers"),
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
								Identifier: []byte("identifiers"),
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
				Identifier: []byte("Identifiers"),
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
			expectedError: "round change justification invalid: invalid message signature: failed to verify signature",
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

	consensusMessage := &specqbft.Message{
		MsgType:    specqbft.PrepareMsgType,
		Height:     1,
		Round:      1,
		Identifier: []byte("Identifier"),
		Data: prepareDataToBytes(t, &specqbft.PrepareData{
			Data: []byte("hello"),
		}),
	}

	inputValue := changeRoundDataToBytes(t, &specqbft.RoundChangeData{
		PreparedValue: []byte("hello"),
		PreparedRound: 1,
		RoundChangeJustification: []*specqbft.SignedMessage{
			protocoltesting.SignMsg(t, sks, []spectypes.OperatorID{operatorIds[0]}, consensusMessage),
			protocoltesting.SignMsg(t, sks, []spectypes.OperatorID{operatorIds[1]}, consensusMessage),
			protocoltesting.SignMsg(t, sks, []spectypes.OperatorID{operatorIds[2]}, consensusMessage),
		},
	})

	round := atomic.Value{}
	round.Store(specqbft.Round(1))

	height := atomic.Value{}
	height.Store(specqbft.Height(1))

	instance := &Instance{
		ContainersMap: map[specqbft.MessageType]msgcont.MessageContainer{
			specqbft.RoundChangeMsgType: msgcontinmem.New(3, 2),
		},
		Config:         qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{Committee: nodes, OperatorIds: shareOperatorIds},
		State: &qbft.State{
			Round:  round,
			Height: height,
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
			Identifier: []byte("Identifier"),
			Data:       changeRoundDataToBytes(t, &specqbft.RoundChangeData{}),
		}

		prepareData, err := msg.GetPrepareData()
		require.NoError(t, err)

		instance.ContainersMap[specqbft.RoundChangeMsgType].AddMessage(SignMsg(t, operatorIds[:1], sks[operatorIds[0]], msg), prepareData.Data)
		instance.ContainersMap[specqbft.RoundChangeMsgType].AddMessage(SignMsg(t, operatorIds[:1], sks[operatorIds[1]], msg), prepareData.Data)

		instance.ContainersMap[specqbft.RoundChangeMsgType].AddMessage(&specqbft.SignedMessage{
			Signature: nil,
			Signers:   operatorIds[:1],
			Message:   msg,
		}, prepareData.Data)
		instance.ContainersMap[specqbft.RoundChangeMsgType].AddMessage(&specqbft.SignedMessage{
			Signature: nil,
			Signers:   operatorIds[1:2],
			Message:   msg,
		}, prepareData.Data)
		instance.ContainersMap[specqbft.RoundChangeMsgType].AddMessage(&specqbft.SignedMessage{
			Signature: nil,
			Signers:   operatorIds[2:3],
			Message:   msg,
		}, prepareData.Data)

		// test no previous prepared round with round change quorum (no justification)
		require.NoError(t, instance.JustifyRoundChange(2))
	})

	t.Run("change round quorum not prepared, instance prepared previously", func(t *testing.T) {
		instance.GetState().PreparedRound.Store(specqbft.Round(1))
		instance.GetState().PreparedValue.Store([]byte("hello"))
		err := instance.JustifyRoundChange(2)
		require.EqualError(t, err, "highest prepared doesn't match prepared state")
	})

	t.Run("change round quorum prepared, instance prepared", func(t *testing.T) {
		instance.ContainersMap[specqbft.RoundChangeMsgType] = msgcontinmem.New(3, 2)

		msg1 := SignMsg(t, operatorIds[:1], sks[operatorIds[0]], &specqbft.Message{
			MsgType:    specqbft.RoundChangeMsgType,
			Height:     1,
			Round:      2,
			Identifier: []byte("Identifier"),
			Data:       inputValue,
		})
		changeRoundData1, err := msg1.Message.GetRoundChangeData()
		require.NoError(t, err)
		instance.ContainersMap[specqbft.RoundChangeMsgType].AddMessage(msg1, changeRoundData1.PreparedValue)

		msg2 := SignMsg(t, operatorIds[1:2], sks[operatorIds[1]], &specqbft.Message{
			MsgType:    specqbft.RoundChangeMsgType,
			Height:     1,
			Round:      2,
			Identifier: []byte("Identifier"),
			Data:       inputValue,
		})
		changeRoundData2, err := msg2.Message.GetRoundChangeData()
		require.NoError(t, err)
		instance.ContainersMap[specqbft.RoundChangeMsgType].AddMessage(msg2, changeRoundData2.PreparedValue)

		msg3 := SignMsg(t, operatorIds[2:3], sks[operatorIds[2]], &specqbft.Message{
			MsgType:    specqbft.RoundChangeMsgType,
			Height:     1,
			Round:      2,
			Identifier: []byte("Identifier"),
			Data:       inputValue,
		})
		changeRoundData3, err := msg3.Message.GetRoundChangeData()
		require.NoError(t, err)
		instance.ContainersMap[specqbft.RoundChangeMsgType].AddMessage(msg3, changeRoundData3.PreparedValue)

		msg4 := SignMsg(t, operatorIds[3:4], sks[operatorIds[3]], &specqbft.Message{
			MsgType:    specqbft.RoundChangeMsgType,
			Height:     1,
			Round:      1,
			Identifier: []byte("Identifier"),
			Data:       inputValue,
		})
		changeRoundData4, err := msg3.Message.GetRoundChangeData()
		require.NoError(t, err)
		instance.ContainersMap[specqbft.RoundChangeMsgType].AddMessage(msg4, changeRoundData4.PreparedValue)

		instance.GetState().Round.Store(specqbft.Round(2))
		// test no previous prepared round with round change quorum (with justification)
		require.NoError(t, instance.JustifyRoundChange(2))
	})
}

func TestHighestPrepared(t *testing.T) {
	secretKeys, nodes, operatorIDs, shareOperatorIDs := GenerateNodes(4)

	inputValue := []byte("input value")

	instance := &Instance{
		Logger: logex.Build(t.Name(), zapcore.DebugLevel, nil),
		ContainersMap: map[specqbft.MessageType]msgcont.MessageContainer{
			specqbft.RoundChangeMsgType: msgcontinmem.New(3, 2),
		},
		State:          &qbft.State{},
		Config:         qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{Committee: nodes, OperatorIds: shareOperatorIDs},
	}
	instance.GetState().Height.Store(specqbft.Height(1))
	instance.GetState().Round.Store(specqbft.Round(3))

	consensusMessage1 := &specqbft.Message{
		MsgType:    specqbft.PrepareMsgType,
		Height:     1,
		Round:      1,
		Identifier: []byte("Identifier"),
		Data: prepareDataToBytes(t, &specqbft.PrepareData{
			Data: inputValue,
		}),
	}

	consensusMessage2 := &specqbft.Message{
		MsgType:    specqbft.PrepareMsgType,
		Height:     1,
		Round:      2,
		Identifier: []byte("Identifier"),
		Data: prepareDataToBytes(t, &specqbft.PrepareData{
			Data: append(inputValue, []byte("highest")...),
		}),
	}

	msg1 := &specqbft.Message{
		MsgType:    specqbft.RoundChangeMsgType,
		Height:     1,
		Round:      3,
		Identifier: []byte("Identifier"),
		Data: changeRoundDataToBytes(t, &specqbft.RoundChangeData{
			PreparedRound: 1,
			PreparedValue: inputValue,
			RoundChangeJustification: []*specqbft.SignedMessage{
				protocoltesting.SignMsg(t, secretKeys, []spectypes.OperatorID{operatorIDs[0]}, consensusMessage1),
				protocoltesting.SignMsg(t, secretKeys, []spectypes.OperatorID{operatorIDs[1]}, consensusMessage1),
				protocoltesting.SignMsg(t, secretKeys, []spectypes.OperatorID{operatorIDs[2]}, consensusMessage1),
			},
		}),
	}

	msg2 := &specqbft.Message{
		MsgType:    specqbft.RoundChangeMsgType,
		Height:     1,
		Round:      3,
		Identifier: []byte("Identifier"),
		Data: changeRoundDataToBytes(t, &specqbft.RoundChangeData{
			PreparedRound: 2,
			PreparedValue: append(inputValue, []byte("highest")...),
			RoundChangeJustification: []*specqbft.SignedMessage{
				protocoltesting.SignMsg(t, secretKeys, []spectypes.OperatorID{operatorIDs[0]}, consensusMessage2),
				protocoltesting.SignMsg(t, secretKeys, []spectypes.OperatorID{operatorIDs[1]}, consensusMessage2),
				protocoltesting.SignMsg(t, secretKeys, []spectypes.OperatorID{operatorIDs[2]}, consensusMessage2),
			},
		}),
	}

	roundChangeData, err := msg1.GetRoundChangeData()
	require.NoError(t, err)

	instance.ContainersMap[specqbft.RoundChangeMsgType].AddMessage(SignMsg(t, operatorIDs[0:1], secretKeys[operatorIDs[0]], msg1), roundChangeData.PreparedValue)
	instance.ContainersMap[specqbft.RoundChangeMsgType].AddMessage(SignMsg(t, operatorIDs[1:2], secretKeys[operatorIDs[1]], msg1), roundChangeData.PreparedValue)
	instance.ContainersMap[specqbft.RoundChangeMsgType].AddMessage(SignMsg(t, operatorIDs[2:3], secretKeys[operatorIDs[2]], msg2), roundChangeData.PreparedValue)

	// test one higher than other
	highest, err := instance.HighestPrepared(3)
	require.NoError(t, err)
	require.NotNil(t, highest)
	require.True(t, highest.Prepared())
	require.EqualValues(t, specqbft.Round(2), highest.PreparedRound)
	require.EqualValues(t, append(inputValue, []byte("highest")...), highest.PreparedValue)

	// test 2 equals
	instance.ContainersMap[specqbft.RoundChangeMsgType].AddMessage(SignMsg(t, operatorIDs[2:3], secretKeys[operatorIDs[2]], msg2), roundChangeData.PreparedValue)

	highest, err = instance.HighestPrepared(3)
	require.NoError(t, err)
	require.NotNil(t, highest)
	require.True(t, highest.Prepared())
	require.EqualValues(t, specqbft.Round(2), highest.PreparedRound)
	require.EqualValues(t, append(inputValue, []byte("highest")...), highest.PreparedValue)
}

func TestChangeRoundMsgValidationPipeline(t *testing.T) {
	sks, nodes, operatorIds, shareOperatorIds := GenerateNodes(4)
	msgID := spectypes.NewMsgID([]byte("Identifier"), spectypes.BNRoleAttester)
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
			"message height is wrong",
		},

		{
			"invalid identifier",
			SignMsg(t, operatorIds[:1], sks[operatorIds[0]], &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Height:     1,
				Round:      1,
				Identifier: []byte("identifiera"),
				Data:       changeRoundDataToBytes(t, &specqbft.RoundChangeData{PreparedValue: nil}),
			}),
			"message identifier (6964656e74696669657261) does not equal expected identifier (4964656e746966696572000000000000000000000000000000000000000000000000000000000000000000000000000000000000)",
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
		State: &qbft.State{
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
		ContainersMap: map[specqbft.MessageType]msgcont.MessageContainer{
			specqbft.PrepareMsgType: msgcontinmem.New(3, 2),
		},
		Config: qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{
			Committee:   nodes,
			PublicKey:   sks[operatorIds[0]].GetPublicKey(), // just placeholder
			OperatorIds: shareOperatorIds,
		},
		State: &qbft.State{
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
		ContainersMap: map[specqbft.MessageType]msgcont.MessageContainer{
			specqbft.PrepareMsgType: msgcontinmem.New(3, 2),
		},
		Config: qbft.DefaultConsensusParams(),
		ValidatorShare: &beacon.Share{
			Committee:   nodes,
			PublicKey:   sks[operatorIds[0]].GetPublicKey(), // just placeholder
			OperatorIds: shareOperatorIds,
		},
		State: &qbft.State{
			Round:  round,
			Height: height,
		},
	}
	instance.fork = testingFork(instance)
	pipeline := instance.ChangeRoundMsgPipeline()
	require.EqualValues(t, "combination of: combination of: basic msg validation, type check, identifier, sequence, authorize, validateJustification msg, , add change round msg, if first pipeline non error, continue to second, upon change round partial quorum, ", pipeline.Name())
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
