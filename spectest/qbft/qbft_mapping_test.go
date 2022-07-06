package qbft

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/qbft/spectest"
	spectests "github.com/bloxapp/ssv-spec/qbft/spectest/tests"
	"github.com/bloxapp/ssv-spec/qbft/spectest/tests/messages"
	"github.com/bloxapp/ssv-spec/qbft/spectest/tests/proposer"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	protocolp2p "github.com/bloxapp/ssv/protocol/v1/p2p"
	qbftprotocol "github.com/bloxapp/ssv/protocol/v1/qbft"
	forksfactory "github.com/bloxapp/ssv/protocol/v1/qbft/controller/forks/factory"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/leader/deterministic"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont"
	"github.com/bloxapp/ssv/protocol/v1/validator"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/logex"
)

func testsToRun() map[string]struct{} {
	testList := spectest.AllTests
	testList = []spectest.SpecTest{
		proposer.FourOperators(),
		proposer.SevenOperators(),
		proposer.TenOperators(),
		proposer.ThirteenOperators(),

		messages.RoundChangeDataInvalidJustifications(),
		messages.RoundChangeDataInvalidPreparedRound(),
		messages.RoundChangeDataInvalidPreparedValue(),
		messages.RoundChangePrePreparedJustifications(),
		messages.RoundChangeNotPreparedJustifications(),
		messages.CommitDataEncoding(),
		messages.DecidedMsgEncoding(),
		messages.MsgNilIdentifier(),
		messages.MsgNonZeroIdentifier(),
		messages.MsgTypeUnknown(),
		messages.PrepareDataEncoding(),
		messages.ProposeDataEncoding(),
		messages.MsgDataNil(),
		messages.MsgDataNonZero(),
		messages.SignedMsgSigTooShort(),
		messages.SignedMsgSigTooLong(),
		messages.SignedMsgNoSigners(),
		messages.GetRoot(),
		messages.SignedMessageEncoding(),
		messages.CreateProposal(),
		messages.CreateProposalPreviouslyPrepared(),
		messages.CreateProposalNotPreviouslyPrepared(),
		messages.CreatePrepare(),
		messages.CreateCommit(),
		messages.CreateRoundChange(),
		messages.CreateRoundChangePreviouslyPrepared(),
		messages.RoundChangeDataEncoding(),

		spectests.HappyFlow(),
		spectests.SevenOperators(),
		spectests.TenOperators(),
		spectests.ThirteenOperators(),
	}

	result := make(map[string]struct{})
	for _, test := range testList {
		result[test.TestName()] = struct{}{}
	}

	return result
}

func TestQBFTMapping(t *testing.T) {
	path, _ := os.Getwd()
	fileName := "tests.json"
	untypedTests := map[string]interface{}{}
	byteValue, err := ioutil.ReadFile(path + "/" + fileName)
	if err != nil {
		panic(err.Error())
	}

	if err := json.Unmarshal(byteValue, &untypedTests); err != nil {
		panic(err.Error())
	}

	testMap := testsToRun() // TODO: remove

	tests := make(map[string]spectest.SpecTest)
	for name, test := range untypedTests {
		name, test := name, test

		testName := strings.Split(name, "_")[1]
		testType := strings.Split(name, "_")[0]

		if _, ok := testMap[testName]; !ok {
			continue
		}

		switch testType {
		case reflect.TypeOf(&spectests.MsgProcessingSpecTest{}).String():
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &spectests.MsgProcessingSpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			t.Run(typedTest.TestName(), func(t *testing.T) {
				runMsgProcessingSpecTest(t, typedTest)
			})
		case reflect.TypeOf(&spectests.MsgSpecTest{}).String():
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &spectests.MsgSpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			tests[testName] = typedTest
			t.Run(typedTest.TestName(), func(t *testing.T) {
				runMsgSpecTest(t, typedTest)
			})
		}
	}
}

func runMsgProcessingSpecTest(t *testing.T, test *spectests.MsgProcessingSpecTest) {
	ctx := context.TODO()
	logger := logex.Build(test.Name, zapcore.DebugLevel, nil)

	forkVersion := forksprotocol.V1ForkVersion
	pi, _ := protocolp2p.GenPeerID()
	p2pNet := protocolp2p.NewMockNetwork(logger, pi, 10)
	beacon := validator.NewTestBeacon(t)
	keysSet := testingutils.Testing4SharesSet()

	db, err := storage.GetStorageFactory(basedb.Options{
		Type:   "badger-memory",
		Logger: logger,
		Ctx:    ctx,
	})
	require.NoError(t, err)

	require.NoError(t, beacon.AddShare(keysSet.Shares[1]))
	require.NoError(t, beacon.AddShare(keysSet.Shares[2]))
	require.NoError(t, beacon.AddShare(keysSet.Shares[3]))
	require.NoError(t, beacon.AddShare(keysSet.Shares[4]))

	share := &beaconprotocol.Share{
		NodeID:    1,
		PublicKey: keysSet.ValidatorPK,
		Committee: map[message.OperatorID]*beaconprotocol.Node{
			1: {
				IbftID: 1,
				Pk:     keysSet.Shares[1].GetPublicKey().Serialize(),
			},
			2: {
				IbftID: 2,
				Pk:     keysSet.Shares[2].GetPublicKey().Serialize(),
			},
			3: {
				IbftID: 3,
				Pk:     keysSet.Shares[3].GetPublicKey().Serialize(),
			},
			4: {
				IbftID: 4,
				Pk:     keysSet.Shares[4].GetPublicKey().Serialize(),
			},
		},
	}

	var mappedCommittee []*types.Operator
	for k, v := range share.Committee {
		mappedCommittee = append(mappedCommittee, &types.Operator{
			OperatorID: types.OperatorID(k),
			PubKey:     v.Pk,
		})
	}

	sort.Slice(mappedCommittee, func(i, j int) bool {
		return mappedCommittee[i].OperatorID < mappedCommittee[j].OperatorID
	})

	mappedShare := &types.Share{
		OperatorID:      types.OperatorID(share.NodeID),
		ValidatorPubKey: share.PublicKey.Serialize(),
		SharePubKey:     share.Committee[share.NodeID].Pk,
		Committee:       mappedCommittee,
		Quorum:          3,
		PartialQuorum:   2,
		DomainType:      types.PrimusTestnet,
		Graffiti:        nil,
	}

	identifier := message.NewIdentifier(share.PublicKey.Serialize(), message.RoleTypeAttester)
	qbftInstance := newQbftInstance(t, logger, p2pNet, beacon, share, forkVersion)

	signatureToSpecSignatureAndID := make(map[string]signatureAndID)

	var lastErr error
	for _, msg := range test.InputMessages {
		origSignAndID := signatureAndID{
			Signature:  msg.Signature,
			Identifier: msg.Message.Identifier,
		}

		msg.Message.Identifier = identifier

		domain := types.PrimusTestnet
		sigType := types.QBFTSignatureType
		r, err := types.ComputeSigningRoot(msg, types.ComputeSignatureDomain(domain, sigType))
		require.NoError(t, err)

		sk := testingutils.Testing4SharesSet().Shares[1]
		sig := sk.SignByte(r)
		serializedSig := sig.Serialize()

		signatureToSpecSignatureAndID[string(serializedSig)] = origSignAndID

		signedMessage := specToSignedMessage(t, msg)
		signedMessage.Signature = serializedSig

		if _, err := qbftInstance.ProcessMsg(signedMessage); err != nil {
			lastErr = err
		}
	}

	mappedInstance := new(qbft.Instance)
	if qbftInstance != nil {
		preparedValue := qbftInstance.State().GetPreparedValue()
		if len(preparedValue) == 0 {
			preparedValue = nil
		}
		round := qbft.Round(qbftInstance.State().GetRound())
		if round == 0 {
			round = 1
		}
		mappedInstance.State = &qbft.State{
			Share:                           mappedShare,
			ID:                              test.Pre.State.ID,
			Round:                           round,
			Height:                          qbft.Height(qbftInstance.State().GetHeight()),
			LastPreparedRound:               qbft.Round(qbftInstance.State().GetPreparedRound()),
			LastPreparedValue:               preparedValue,
			ProposalAcceptedForCurrentRound: test.Pre.State.ProposalAcceptedForCurrentRound,
			//Decided:                         decided != nil && decided.Message.Height == qbftInstance.State().GetHeight(), // TODO might need to add this flag to qbftCtrl
			//DecidedValue:                    decidedValue,                                                                    // TODO allow a way to get it
			ProposeContainer:     convertToSpecContainer(t, qbftInstance.Containers()[qbft.ProposalMsgType], signatureToSpecSignatureAndID),
			PrepareContainer:     convertToSpecContainer(t, qbftInstance.Containers()[qbft.PrepareMsgType], signatureToSpecSignatureAndID),
			CommitContainer:      convertToSpecContainer(t, qbftInstance.Containers()[qbft.CommitMsgType], signatureToSpecSignatureAndID),
			RoundChangeContainer: convertToSpecContainer(t, qbftInstance.Containers()[qbft.RoundChangeMsgType], signatureToSpecSignatureAndID),
		}
		mappedInstance.StartValue = qbftInstance.State().GetInputValue()
	}

	// TODO: 1) check the state of qbft instance; 2) check `test.OutputMessages`

	if len(test.ExpectedError) != 0 {
		require.EqualError(t, lastErr, test.ExpectedError)
	} else {
		require.NoError(t, lastErr)
	}

	mappedRoot, err := mappedInstance.State.GetRoot()
	require.NoError(t, err)
	require.Equal(t, test.PostRoot, hex.EncodeToString(mappedRoot))

	type BroadcastMessagesGetter interface {
		GetBroadcastMessages() []message.SSVMessage
	}

	outputMessages := p2pNet.(BroadcastMessagesGetter).GetBroadcastMessages()

	require.Equal(t, len(test.OutputMessages), len(outputMessages))

	for i, outputMessage := range outputMessages {
		signedMessage := test.OutputMessages[i]
		signedMessage.Message.Identifier = identifier
		require.Equal(t, outputMessage, specToSignedMessage(t, signedMessage))
	}

	db.Close()
}

func runMsgSpecTest(t *testing.T, test *spectests.MsgSpecTest) {
	var lastErr error

	for i, messageBytes := range test.EncodedMessages {
		m := &qbft.SignedMessage{}
		if err := m.Decode(messageBytes); err != nil {
			lastErr = err
		}

		if len(test.ExpectedRoots) > 0 {
			r, err := specToSignedMessage(t, m).GetRoot(forksprotocol.V1ForkVersion.String())
			if err != nil {
				lastErr = err
			}

			if !bytes.Equal(test.ExpectedRoots[i], r) {
				t.Fail()
			}
		}
	}

	for i, msg := range test.Messages {
		if err := msg.Validate(); err != nil {
			lastErr = err
		}

		switch msg.Message.MsgType {
		case qbft.RoundChangeMsgType:
			rc := qbft.RoundChangeData{}
			if err := rc.Decode(msg.Message.Data); err != nil {
				lastErr = err
			}
			if err := validateRoundChangeData(specToRoundChangeData(t, rc)); err != nil {
				lastErr = err
			}
		}

		if len(test.Messages) > 0 {
			r1, err := msg.Encode()
			if err != nil {
				lastErr = err
			}

			r2, err := test.Messages[i].Encode()
			if err != nil {
				lastErr = err
			}
			if !bytes.Equal(r2, r1) {
				t.Fail()
			}
		}
	}

	if len(test.ExpectedError) != 0 {
		require.EqualError(t, lastErr, test.ExpectedError)
	} else {
		require.NoError(t, lastErr)
	}
}

func validateRoundChangeData(d message.RoundChangeData) error {
	if isRoundChangeDataPrepared(d) {
		if len(d.PreparedValue) == 0 {
			return errors.New("round change prepared value invalid")
		}
		if len(d.RoundChangeJustification) == 0 {
			return errors.New("round change justification invalid")
		}
		// TODO - should next proposal data be equal to prepared value?
	}

	if len(d.NextProposalData) == 0 {
		return errors.New("round change next value invalid")
	}
	return nil
}

func isRoundChangeDataPrepared(d message.RoundChangeData) bool {
	return d.Round != 0 || len(d.PreparedValue) != 0
}

type signatureAndID struct {
	Signature  []byte
	Identifier []byte
}

func newQbftInstance(t *testing.T, logger *zap.Logger, net protocolp2p.MockNetwork, beacon *validator.TestBeacon, share *beaconprotocol.Share, forkVersion forksprotocol.ForkVersion) instance.Instancer {
	const height = 0
	fork := forksfactory.NewFork(forkVersion)
	identifier := message.NewIdentifier(share.PublicKey.Serialize(), message.RoleTypeAttester)
	leaderSelectionSeed := append(fork.Identifier(identifier.GetValidatorPK(), identifier.GetRoleType()), []byte(strconv.FormatUint(uint64(height), 10))...)
	leaderSelc, err := deterministic.New(leaderSelectionSeed, uint64(share.CommitteeSize()))
	require.NoError(t, err)

	return instance.NewInstance(&instance.Options{
		Logger:           logger,
		ValidatorShare:   share,
		Network:          net,
		LeaderSelector:   leaderSelc,
		Config:           qbftprotocol.DefaultConsensusParams(),
		Identifier:       identifier,
		Height:           height,
		RequireMinPeers:  false,
		Fork:             fork.InstanceFork(),
		Signer:           beacon,
		ChangeRoundStore: nil,
	})
}

func specToSignedMessage(t *testing.T, msg *qbft.SignedMessage) *message.SignedMessage {
	signers := make([]message.OperatorID, 0)
	for _, signer := range msg.Signers {
		signers = append(signers, message.OperatorID(signer))
	}

	var messageType message.ConsensusMessageType

	switch msg.Message.MsgType {
	case qbft.ProposalMsgType:
		messageType = message.ProposalMsgType
	case qbft.PrepareMsgType:
		messageType = message.PrepareMsgType
	case qbft.CommitMsgType:
		messageType = message.CommitMsgType
	case qbft.RoundChangeMsgType:
		messageType = message.RoundChangeMsgType
	}

	domain := types.PrimusTestnet
	sigType := types.QBFTSignatureType
	r, err := types.ComputeSigningRoot(msg, types.ComputeSignatureDomain(domain, sigType))
	require.NoError(t, err)

	sk := testingutils.Testing4SharesSet().Shares[1]
	sig := sk.SignByte(r)

	return &message.SignedMessage{
		Signature: sig.Serialize(),
		//Signature: []byte(msg.Signature),
		Signers: signers,
		Message: &message.ConsensusMessage{
			MsgType:    messageType,
			Height:     message.Height(msg.Message.Height),
			Round:      message.Round(msg.Message.Round),
			Identifier: msg.Message.Identifier,
			//Identifier: msg.Message.Identifier,
			Data: msg.Message.Data,
		},
	}
}

func specToRoundChangeData(t *testing.T, spec qbft.RoundChangeData) message.RoundChangeData {
	rcj := make([]*message.SignedMessage, 0, len(spec.RoundChangeJustification))
	for _, v := range spec.RoundChangeJustification {
		rcj = append(rcj, specToSignedMessage(t, v))
	}

	return message.RoundChangeData{
		PreparedValue:            spec.PreparedValue,
		Round:                    message.Round(spec.PreparedRound),
		NextProposalData:         spec.NextProposalData,
		RoundChangeJustification: rcj,
	}
}

func convertToSpecContainer(t *testing.T, container msgcont.MessageContainer, signatureToSpecSignatureAndID map[string]signatureAndID) *qbft.MsgContainer {
	c := qbft.NewMsgContainer()
	container.AllMessaged(func(round message.Round, msg *message.SignedMessage) {
		var signers []types.OperatorID
		for _, s := range msg.GetSigners() {
			signers = append(signers, types.OperatorID(s))
		}

		originalSignatureAndID := signatureToSpecSignatureAndID[string(msg.Signature)]

		// TODO need to use one of the message type (spec/protocol)
		ok, err := c.AddIfDoesntExist(&qbft.SignedMessage{
			Signature: originalSignatureAndID.Signature,
			Signers:   signers,
			Message: &qbft.Message{
				MsgType:    qbft.MessageType(msg.Message.MsgType),
				Height:     qbft.Height(msg.Message.Height),
				Round:      qbft.Round(msg.Message.Round),
				Identifier: originalSignatureAndID.Identifier,
				Data:       msg.Message.Data,
			},
		})
		require.NoError(t, err)
		require.True(t, ok)
	})
	return c
}
