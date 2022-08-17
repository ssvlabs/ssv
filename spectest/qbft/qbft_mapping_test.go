package qbft

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/qbft/spectest"
	spectests "github.com/bloxapp/ssv-spec/qbft/spectest/tests"
	"github.com/bloxapp/ssv-spec/qbft/spectest/tests/commit"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	protocolp2p "github.com/bloxapp/ssv/protocol/v1/p2p"
	qbftprotocol "github.com/bloxapp/ssv/protocol/v1/qbft"
	forksfactory "github.com/bloxapp/ssv/protocol/v1/qbft/controller/forks/factory"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/forks"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/leader/constant"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/proposal"
	"github.com/bloxapp/ssv/protocol/v1/types"
	"github.com/bloxapp/ssv/protocol/v1/validator"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/logex"
)

// nolint
func testsToRun() map[string]struct{} {
	tests := make(map[string]struct{})

	testsBrokenInSpec := map[string]struct{}{
		commit.FutureDecided().TestName(): {},
	}

	for _, testItem := range spectest.AllTests {
		if _, exists := testsBrokenInSpec[testItem.TestName()]; !exists {
			tests[testItem.TestName()] = struct{}{}
		}
	}

	return tests
}

func TestQBFTMapping(t *testing.T) {
	path, _ := os.Getwd()
	fileName := "tests.json"
	filePath := path + "/" + fileName
	jsonTests, err := ioutil.ReadFile(filePath)
	if err != nil {
		resp, err := http.Get("https://raw.githubusercontent.com/bloxapp/ssv-spec/align-error-messages/qbft/spectest/generate/tests.json")
		require.NoError(t, err)

		defer func() {
			require.NoError(t, resp.Body.Close())
		}()

		jsonTests, err = ioutil.ReadAll(resp.Body)
		require.NoError(t, err)

		require.NoError(t, ioutil.WriteFile(filePath, jsonTests, 0644))
	}

	untypedTests := map[string]interface{}{}
	if err := json.Unmarshal(jsonTests, &untypedTests); err != nil {
		panic(err.Error())
	}

	origDomain := types.GetDefaultDomain()
	types.SetDefaultDomain(spectypes.PrimusTestnet)
	defer func() {
		types.SetDefaultDomain(origDomain)
	}()

	testMap := testsToRun() // TODO: Remove overriding after commit.FutureDecided() is fixed in spec.

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

	forkVersion := forksprotocol.GenesisForkVersion
	pi, _ := protocolp2p.GenPeerID()
	p2pNet := protocolp2p.NewMockNetwork(logger, pi, 10)
	beacon := validator.NewTestBeacon(t)

	var keysSet *testingutils.TestKeySet
	switch len(test.Pre.State.Share.Committee) {
	case 4:
		keysSet = testingutils.Testing4SharesSet()
	case 7:
		keysSet = testingutils.Testing7SharesSet()
	case 10:
		keysSet = testingutils.Testing10SharesSet()
	case 13:
		keysSet = testingutils.Testing13SharesSet()
	default:
		t.Error("unknown key set length")
	}

	db, err := storage.GetStorageFactory(basedb.Options{
		Type:   "badger-memory",
		Logger: logger,
		Ctx:    ctx,
	})
	require.NoError(t, err)

	qbftStorage := qbftstorage.NewQBFTStore(db, logger, spectypes.BNRoleAttester.String())

	share := &beaconprotocol.Share{
		NodeID:    1,
		PublicKey: keysSet.ValidatorPK,
		Committee: make(map[spectypes.OperatorID]*beaconprotocol.Node),
	}

	var mappedCommittee []*spectypes.Operator
	for i := range test.Pre.State.Share.Committee {
		operatorID := spectypes.OperatorID(i) + 1
		require.NoError(t, beacon.AddShare(keysSet.Shares[operatorID]))

		pk := keysSet.Shares[operatorID].GetPublicKey().Serialize()
		share.Committee[spectypes.OperatorID(i)+1] = &beaconprotocol.Node{
			IbftID: uint64(i) + 1,
			Pk:     pk,
		}

		share.OperatorIds = append(share.OperatorIds, uint64(i)+1)

		mappedCommittee = append(mappedCommittee, &spectypes.Operator{
			OperatorID: operatorID,
			PubKey:     pk,
		})
	}

	mappedShare := &spectypes.Share{
		OperatorID:      share.NodeID,
		ValidatorPubKey: share.PublicKey.Serialize(),
		SharePubKey:     share.Committee[share.NodeID].Pk,
		Committee:       mappedCommittee,
		Quorum:          keysSet.Threshold,
		PartialQuorum:   keysSet.PartialThreshold,
		DomainType:      types.GetDefaultDomain(),
		Graffiti:        nil,
	}

	qbftInstance := newQbftInstance(logger, qbftStorage, p2pNet, beacon, share, mappedShare, test.Pre.StartValue, forkVersion)
	qbftInstance.Init()
	qbftInstance.State().InputValue.Store(test.Pre.StartValue)
	qbftInstance.State().Round.Store(test.Pre.State.Round)
	qbftInstance.State().Height.Store(test.Pre.State.Height)
	qbftInstance.State().ProposalAcceptedForCurrentRound.Store(test.Pre.State.ProposalAcceptedForCurrentRound)

	var lastErr error
	for _, msg := range test.InputMessages {
		if _, err := qbftInstance.ProcessMsg(msg); err != nil {
			lastErr = err
		}
	}

	mappedInstance := new(specqbft.Instance)
	if qbftInstance != nil {
		preparedValue := qbftInstance.State().GetPreparedValue()
		if len(preparedValue) == 0 {
			preparedValue = nil
		}
		round := qbftInstance.State().GetRound()

		_, decidedErr := qbftInstance.CommittedAggregatedMsg()
		var decidedValue []byte
		if decidedErr == nil && len(test.InputMessages) != 0 {
			decidedValue = test.InputMessages[0].Message.Identifier
		}

		mappedInstance.State = &specqbft.State{
			Share:                           mappedShare,
			ID:                              test.Pre.State.ID,
			Round:                           round,
			Height:                          qbftInstance.State().GetHeight(),
			LastPreparedRound:               qbftInstance.State().GetPreparedRound(),
			LastPreparedValue:               preparedValue,
			ProposalAcceptedForCurrentRound: qbftInstance.State().GetProposalAcceptedForCurrentRound(),
			Decided:                         decidedErr == nil,
			DecidedValue:                    decidedValue,
			ProposeContainer:                convertToSpecContainer(t, qbftInstance.Containers()[specqbft.ProposalMsgType]),
			PrepareContainer:                convertToSpecContainer(t, qbftInstance.Containers()[specqbft.PrepareMsgType]),
			CommitContainer:                 convertToSpecContainer(t, qbftInstance.Containers()[specqbft.CommitMsgType]),
			RoundChangeContainer:            convertToSpecContainer(t, qbftInstance.Containers()[specqbft.RoundChangeMsgType]),
		}

		mappedInstance.StartValue = qbftInstance.State().GetInputValue()
	}

	if len(test.ExpectedError) != 0 {
		require.EqualError(t, lastErr, test.ExpectedError)
	} else {
		require.NoError(t, lastErr)
	}

	mappedRoot, err := mappedInstance.State.GetRoot()
	require.NoError(t, err)
	require.Equal(t, test.PostRoot, hex.EncodeToString(mappedRoot))

	type BroadcastMessagesGetter interface {
		GetBroadcastMessages() []spectypes.SSVMessage
	}

	outputMessages := p2pNet.(BroadcastMessagesGetter).GetBroadcastMessages()

	require.Equal(t, len(test.OutputMessages), len(outputMessages))

	for i, outputMessage := range outputMessages {
		r1, _ := test.OutputMessages[i].GetRoot()

		msg2 := &specqbft.SignedMessage{}
		require.NoError(t, msg2.Decode(outputMessage.Data))

		r2, _ := msg2.GetRoot()
		require.EqualValues(t, r1, r2, fmt.Sprintf("output msg %d roots not equal", i))
	}

	db.Close()
}

type forkWithValueCheck struct {
	forks.Fork
}

func (f forkWithValueCheck) ProposalMsgValidationPipeline(share *beaconprotocol.Share, state *qbftprotocol.State, roundLeader proposal.LeaderResolver) pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		f.Fork.ProposalMsgValidationPipeline(share, state, roundLeader),
		msgValueCheck(),
	)
}

func msgValueCheck() pipelines.SignedMessagePipeline {
	return pipelines.WrapFunc("value check", func(signedMessage *specqbft.SignedMessage) error {
		if signedMessage.Message.MsgType != specqbft.ProposalMsgType {
			return nil
		}

		proposalData, err := signedMessage.Message.GetProposalData()
		if err != nil {
			return nil
		}
		if err := proposalData.Validate(); err != nil {
			return nil
		}

		if err := testingutils.TestingConfig(testingutils.Testing4SharesSet()).ValueCheckF(proposalData.Data); err != nil {
			return fmt.Errorf("proposal not justified: proposal value invalid: %w", err)
		}

		return nil
	})
}

func runMsgSpecTest(t *testing.T, test *spectests.MsgSpecTest) {
	var lastErr error

	for i, messageBytes := range test.EncodedMessages {
		m := &specqbft.SignedMessage{}
		if err := m.Decode(messageBytes); err != nil {
			lastErr = err
		}

		if len(test.ExpectedRoots) > 0 {
			r, err := m.GetRoot()
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
		case specqbft.RoundChangeMsgType:
			rc := specqbft.RoundChangeData{}
			if err := rc.Decode(msg.Message.Data); err != nil {
				lastErr = err
			}
			if err := validateRoundChangeData(rc); err != nil {
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

func validateRoundChangeData(d specqbft.RoundChangeData) error {
	if isRoundChangeDataPrepared(d) {
		if len(d.PreparedValue) == 0 {
			return errors.New("round change prepared value invalid")
		}
		if len(d.RoundChangeJustification) == 0 {
			return errors.New("round change justification invalid")
		}
	}

	return nil
}

func isRoundChangeDataPrepared(d specqbft.RoundChangeData) bool {
	return d.PreparedRound != 0 || len(d.PreparedValue) != 0
}

// nolint:unused
type roundRobinLeaderSelector struct {
	state       *qbftprotocol.State
	mappedShare *spectypes.Share
}

// nolint:unused
func (m roundRobinLeaderSelector) Calculate(round uint64) uint64 {
	specState := &specqbft.State{
		Share:  m.mappedShare,
		Height: m.state.GetHeight(),
	}
	// RoundRobinProposer returns OperatorID which starts with 1.
	// As the result will be used to index OperatorIds which starts from 0,
	// the result of Calculate should be decremented.
	return uint64(specqbft.RoundRobinProposer(specState, specqbft.Round(round))) - 1
}

func newQbftInstance(logger *zap.Logger, qbftStorage qbftstorage.QBFTStore, net protocolp2p.MockNetwork, beacon *validator.TestBeacon, share *beaconprotocol.Share, mappedShare *spectypes.Share, identifier []byte, forkVersion forksprotocol.ForkVersion) instance.Instancer {
	const height = 0
	fork := forksfactory.NewFork(forkVersion)

	newInstance := instance.NewInstance(&instance.Options{
		Logger:           logger,
		ValidatorShare:   share,
		Network:          net,
		Config:           qbftprotocol.DefaultConsensusParams(),
		Identifier:       identifier[:],
		Height:           height,
		RequireMinPeers:  false,
		Fork:             forkWithValueCheck{fork.InstanceFork()},
		SSVSigner:        beacon.KeyManager,
		ChangeRoundStore: qbftStorage,
	})
	//newInstance.(*instance.Instance).LeaderSelector = roundRobinLeaderSelector{newInstance.State(), mappedShare}
	// TODO(nkryuchkov): replace when ready
	newInstance.(*instance.Instance).LeaderSelector = &constant.Constant{
		LeaderIndex: 0,
		OperatorIDs: share.OperatorIds,
	}
	return newInstance
}

func convertToSpecContainer(t *testing.T, container msgcont.MessageContainer) *specqbft.MsgContainer {
	c := specqbft.NewMsgContainer()
	container.AllMessaged(func(round specqbft.Round, msg *specqbft.SignedMessage) {
		ok, err := c.AddIfDoesntExist(&specqbft.SignedMessage{
			Signature: msg.Signature,
			Signers:   msg.Signers,
			Message: &specqbft.Message{
				MsgType:    msg.Message.MsgType,
				Height:     msg.Message.Height,
				Round:      msg.Message.Round,
				Identifier: msg.Message.Identifier,
				Data:       msg.Message.Data,
			},
		})
		require.NoError(t, err)
		require.True(t, ok)
	})
	return c
}
