package ssv

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/ssv"
	"github.com/bloxapp/ssv-spec/ssv/spectest/tests"
	"github.com/bloxapp/ssv-spec/ssv/spectest/tests/consensus/attester"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"

	qbftStorage "github.com/bloxapp/ssv/ibft/storage"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	protocolp2p "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/bloxapp/ssv/protocol/v1/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont"
	"github.com/bloxapp/ssv/protocol/v1/validator"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/logex"
)

func TestSSVMapping(t *testing.T) {
	path, _ := os.Getwd()
	fileName := "tests.json"
	specTests := map[string]*tests.SpecTest{}
	byteValue, err := ioutil.ReadFile(path + "/" + fileName)
	require.NoError(t, err)

	if err := json.Unmarshal(byteValue, &specTests); err != nil {
		require.NoError(t, err)
	}

	testMap := testsToRun() // TODO: remove

	for _, test := range specTests {
		test := test
		if _, ok := testMap[test.Name]; !ok {
			continue
		}

		t.Run(test.Name, func(t *testing.T) {
			runMappingTest(t, test)
		})
	}
}

func testsToRun() map[string]struct{} {
	//testList := spectest.AllTests
	testList := []*tests.SpecTest{attester.HappyFlow()}

	result := make(map[string]struct{})
	for _, test := range testList {
		result[test.Name] = struct{}{}
	}

	return result
}

func runMappingTest(t *testing.T, test *tests.SpecTest) {
	ctx := context.TODO()
	logger := logex.Build(test.Name, zapcore.DebugLevel, nil)

	forkVersion := forksprotocol.V1ForkVersion
	pi, _ := protocolp2p.GenPeerID()
	beacon := validator.NewTestBeacon(t)

	keysSet := testingutils.Testing4SharesSet()

	beaconNetwork := core.NetworkFromString(string(test.Runner.BeaconNetwork))
	if beaconNetwork == "" {
		beaconNetwork = core.PraterNetwork
	}

	db, err := storage.GetStorageFactory(basedb.Options{
		Type:   "badger-memory",
		Logger: logger,
		Ctx:    ctx,
	})
	require.NoError(t, err)

	beaconRoleType := convertFromSpecRole(test.Runner.BeaconRoleType)
	//require.Equalf(t, message.RoleTypeAttester, beaconRoleType, "only attester role is supported now")

	ibftStorage := qbftStorage.New(db, logger, beaconRoleType.String(), forkVersion)
	require.NoError(t, beacon.AddShare(keysSet.Shares[1]))
	require.NoError(t, beacon.AddShare(keysSet.Shares[2]))
	require.NoError(t, beacon.AddShare(keysSet.Shares[3]))
	require.NoError(t, beacon.AddShare(keysSet.Shares[4]))

	// TODO: add an option: array of duty roles, setup ibfts depending on that.
	const attesterRoleType = message.RoleTypeAttester

	for i := 0; i < 4; i++ {
		fmt.Printf("keysSet.Shares[%d].GetPublicKey().Serialize(): %v\n", i+1, hex.EncodeToString(keysSet.Shares[types.OperatorID(i+1)].GetPublicKey().Serialize()))
		fmt.Printf("test.Runner.Share.Committee[%d].GetPublicKey(): %v\n", i, hex.EncodeToString(test.Runner.Share.Committee[i].GetPublicKey()))
	}

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

	v := validator.NewValidator(&validator.Options{
		Context:                    ctx,
		Logger:                     logger,
		IbftStorage:                ibftStorage,
		Network:                    beaconprotocol.NewNetwork(beaconNetwork),
		P2pNetwork:                 protocolp2p.NewMockNetwork(logger, pi, 10),
		Beacon:                     beacon,
		Share:                      share,
		ForkVersion:                forkVersion,
		Signer:                     beacon,
		SyncRateLimit:              time.Second * 5,
		SignatureCollectionTimeout: time.Second * 5,
		ReadMode:                   false,
		FullNode:                   false,
		DutyRoles:                  []message.RoleType{attesterRoleType},
	})

	qbftCtrl := v.(*validator.Validator).Ibfts()[attesterRoleType].(*controller.Controller)
	qbftCtrl.State = controller.Ready
	go qbftCtrl.StartQueueConsumer(qbftCtrl.MessageHandler)
	require.NoError(t, qbftCtrl.Init())
	go v.ExecuteDuty(12, convertDuty(test.Duty))

	for _, msg := range test.Messages {
		require.NoError(t, v.ProcessMsg(convertSSVMessage(t, msg, attesterRoleType)))
	}

	time.Sleep(time.Second * 3) // 3s round

	currentInstance := qbftCtrl.GetCurrentInstance()
	decided, err := ibftStorage.GetLastDecided(qbftCtrl.GetIdentifier())
	require.NoError(t, err)
	decidedValue := []byte("")
	if decided != nil {
		cd, err := decided.Message.GetCommitData()
		require.NoError(t, err)
		decidedValue = cd.Data
	}

	mappedInstance := new(qbft.Instance)
	if currentInstance != nil {
		mappedInstance.State = &qbft.State{
			Share:                           test.Runner.Share,
			ID:                              qbftCtrl.GetIdentifier(),
			Round:                           qbft.Round(currentInstance.State().GetRound()),
			Height:                          qbft.Height(currentInstance.State().GetHeight()),
			LastPreparedRound:               qbft.Round(currentInstance.State().GetPreparedRound()),
			LastPreparedValue:               currentInstance.State().GetPreparedValue(),
			ProposalAcceptedForCurrentRound: nil,
			Decided:                         decided != nil && decided.Message.Height == currentInstance.State().GetHeight(), // TODO might need to add this flag to qbftCtrl
			DecidedValue:                    decidedValue,                                                                    // TODO allow a way to get it
			ProposeContainer:                convertToSpecContainer(t, currentInstance.Containers()[qbft.ProposalMsgType]),
			PrepareContainer:                convertToSpecContainer(t, currentInstance.Containers()[qbft.PrepareMsgType]),
			CommitContainer:                 convertToSpecContainer(t, currentInstance.Containers()[qbft.CommitMsgType]),
			RoundChangeContainer:            convertToSpecContainer(t, currentInstance.Containers()[qbft.RoundChangeMsgType]),
		}
		mappedInstance.StartValue = currentInstance.State().GetInputValue()
	}

	mappedDecidedValue := &types.ConsensusData{
		Duty: &types.Duty{
			Type:                    0,
			PubKey:                  phase0.BLSPubKey{},
			Slot:                    0,
			ValidatorIndex:          0,
			CommitteeIndex:          0,
			CommitteeLength:         0,
			CommitteesAtSlot:        0,
			ValidatorCommitteeIndex: 0,
		},
		AttestationData:           nil,
		BlockData:                 nil,
		AggregateAndProof:         nil,
		SyncCommitteeBlockRoot:    phase0.Root{},
		SyncCommitteeContribution: nil,
	}

	mappedSignedAtts := &phase0.Attestation{
		AggregationBits: nil,
		Data:            nil,
		Signature:       phase0.BLSSignature{},
	}

	resState := ssv.NewDutyExecutionState(3)
	resState.RunningInstance = mappedInstance
	resState.DecidedValue = mappedDecidedValue
	resState.SignedAttestation = mappedSignedAtts
	resState.Finished = true // TODO need to set real value

	root, err := resState.GetRoot()
	require.NoError(t, err)

	expectedRoot, err := hex.DecodeString(test.PostDutyRunnerStateRoot)
	require.NoError(t, err)
	require.Equal(t, expectedRoot, root)

	require.NoError(t, v.Close())
	db.Close()
}

func convertDuty(duty *types.Duty) *beaconprotocol.Duty {
	return &beaconprotocol.Duty{
		Type:                    convertFromSpecRole(duty.Type),
		PubKey:                  duty.PubKey,
		Slot:                    duty.Slot,
		ValidatorIndex:          duty.ValidatorIndex,
		CommitteeIndex:          duty.CommitteeIndex,
		CommitteeLength:         duty.CommitteeLength,
		CommitteesAtSlot:        duty.CommitteesAtSlot,
		ValidatorCommitteeIndex: duty.ValidatorCommitteeIndex,
	}
}

func convertFromSpecRole(role types.BeaconRole) message.RoleType {
	switch role {
	case types.BNRoleAttester:
		return message.RoleTypeAttester
	case types.BNRoleAggregator:
		return message.RoleTypeAggregator
	case types.BNRoleProposer:
		return message.RoleTypeProposer
	case types.BNRoleSyncCommittee, types.BNRoleSyncCommitteeContribution:
		return message.RoleTypeUnknown
	default:
		panic(fmt.Sprintf("unknown role type! (%s)", role.String()))
	}
}

func convertSSVMessage(t *testing.T, msg *types.SSVMessage, role message.RoleType) *message.SSVMessage {
	data := msg.Data

	var msgType message.MsgType
	switch msg.GetType() {
	case types.SSVConsensusMsgType:
		msgType = message.SSVConsensusMsgType
	case types.SSVDecidedMsgType:
		msgType = message.SSVDecidedMsgType
	case types.SSVPartialSignatureMsgType:
		encoded, err := msg.Encode()
		require.NoError(t, err)
		data = encoded
	case types.DKGMsgType:
		panic("type not supported yet")
	}
	return &message.SSVMessage{
		MsgType: msgType,
		ID:      message.NewIdentifier(msg.MsgID.GetPubKey()[:], role),
		Data:    data,
	}
}

func convertToSpecContainer(t *testing.T, container msgcont.MessageContainer) *qbft.MsgContainer {
	c := qbft.NewMsgContainer()
	container.AllMessaged(func(round message.Round, msg *message.SignedMessage) {
		var signers []types.OperatorID
		for _, s := range msg.GetSigners() {
			signers = append(signers, types.OperatorID(s))
		}

		// TODO need to use one of the message type (spec/protocol)
		ok, err := c.AddIfDoesntExist(&qbft.SignedMessage{
			Signature: types.Signature(msg.Signature),
			Signers:   signers,
			Message: &qbft.Message{
				MsgType:    qbft.MessageType(msg.Message.MsgType),
				Height:     qbft.Height(msg.Message.Height),
				Round:      qbft.Round(msg.Message.Round),
				Identifier: msg.Message.Identifier,
				Data:       msg.Message.Data,
			},
		})
		require.NoError(t, err)
		require.True(t, ok)
	})
	return c
}

func convertSingers(specSigners []types.OperatorID) []message.OperatorID {
	var signers []message.OperatorID
	for _, s := range specSigners {
		signers = append(signers, message.OperatorID(s))
	}
	return signers
}
