package qbft

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/qbft/spectest"
	spectests "github.com/bloxapp/ssv-spec/qbft/spectest/tests"
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
	"github.com/bloxapp/ssv/protocol/v1/validator"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/logex"
)

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

	tests := make(map[string]spectest.SpecTest)
	for name, test := range untypedTests {
		testName := strings.Split(name, "_")[1]
		testType := strings.Split(name, "_")[0]
		switch testType {
		case reflect.TypeOf(&spectests.MsgProcessingSpecTest{}).String():
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &spectests.MsgProcessingSpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			//// a little trick we do to instantiate all the internal instance params
			//preByts, _ := typedTest.Pre.Encode()
			//pre := qbft.NewInstance(testingutils.TestingConfig(testingutils.Testing4SharesSet()), typedTest.Pre.State.Share, typedTest.Pre.State.ID, qbft.FirstHeight)
			//pre.Decode(preByts)
			//typedTest.Pre = pre
			//
			//tests[testName] = typedTest
			t.Run(typedTest.TestName(), func(t *testing.T) {
				//typedTest.Run(t)
				runMsgProcessingSpecTest(t, typedTest)
			})
		case reflect.TypeOf(&spectests.MsgSpecTest{}).String():
			byts, err := json.Marshal(test)
			require.NoError(t, err)
			typedTest := &spectests.MsgSpecTest{}
			require.NoError(t, json.Unmarshal(byts, &typedTest))

			tests[testName] = typedTest
			t.Run(typedTest.TestName(), func(t *testing.T) {
				//typedTest.Run(t)
				//runMsgSpecTest(t, typedTest)
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

	identifier := message.NewIdentifier(share.PublicKey.Serialize(), message.RoleTypeAttester)
	qbftInstance := newQbftInstance(t, logger, p2pNet, beacon, share, forkVersion)

	for _, msg := range test.InputMessages {
		_, err := qbftInstance.ProcessMsg(convertSignedMessage(t, msg, identifier))
		require.NoError(t, err)
	}

	time.Sleep(time.Second * 3) // 3s round

	db.Close()
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

func convertSignedMessage(t *testing.T, msg *qbft.SignedMessage, identifier message.Identifier) *message.SignedMessage {
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

	msg.Message.Identifier = identifier

	domain := types.PrimusTestnet
	sigType := types.QBFTSignatureType
	r, err := types.ComputeSigningRoot(msg, types.ComputeSignatureDomain(domain, sigType))
	require.NoError(t, err)

	sk := testingutils.Testing4SharesSet().Shares[1]
	sig := sk.SignByte(r)

	return &message.SignedMessage{
		Signature: sig.Serialize(),
		Signers:   signers,
		Message: &message.ConsensusMessage{
			MsgType:    messageType,
			Height:     message.Height(msg.Message.Height),
			Round:      message.Round(msg.Message.Round),
			Identifier: identifier,
			//Identifier: msg.Message.Identifier,
			Data: msg.Message.Data,
		},
	}
}

func convertSingers(specSigners []types.OperatorID) []message.OperatorID {
	var signers []message.OperatorID
	for _, s := range specSigners {
		signers = append(signers, message.OperatorID(s))
	}
	return signers
}
