package ssv

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv-spec/qbft"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	specssv "github.com/bloxapp/ssv-spec/ssv"
	"github.com/bloxapp/ssv-spec/ssv/spectest/tests"
	"github.com/bloxapp/ssv-spec/ssv/spectest/tests/consensus/attester"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"

	qbftStorage "github.com/bloxapp/ssv/ibft/storage"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	protocolp2p "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/bloxapp/ssv/protocol/v1/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont"
	"github.com/bloxapp/ssv/protocol/v1/validator"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/logex"
)

func TestSSVMapping(t *testing.T) {
	// TODO(nkryuchkov): fix
	t.Skip()

	resp, err := http.Get("https://raw.githubusercontent.com/bloxapp/ssv-spec/main/ssv/spectest/generate/tests.json")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, resp.Body.Close())
	}()

	jsonTests, err := ioutil.ReadAll(resp.Body)
	require.NoError(t, err)

	specTests := map[string]*tests.SpecTest{}
	if err := json.Unmarshal(jsonTests, &specTests); err != nil {
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

// nolint: unused
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

	forkVersion := forksprotocol.GenesisForkVersion
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

	//require.Equalf(t, spectypes.BNRoleAttester, beaconRoleType, "only attester role is supported now")

	ibftStorage := qbftStorage.New(db, logger, test.Runner.BeaconRoleType.String(), forkVersion)
	require.NoError(t, beacon.AddShare(keysSet.Shares[1]))
	require.NoError(t, beacon.AddShare(keysSet.Shares[2]))
	require.NoError(t, beacon.AddShare(keysSet.Shares[3]))
	require.NoError(t, beacon.AddShare(keysSet.Shares[4]))

	// TODO: add an option: array of duty roles, setup ibfts depending on that.
	const attesterRoleType = spectypes.BNRoleAttester

	for i := 0; i < 4; i++ {
		fmt.Printf("keysSet.Shares[%d].GetPublicKey().Serialize(): %v\n", i+1, hex.EncodeToString(keysSet.Shares[spectypes.OperatorID(i+1)].GetPublicKey().Serialize()))
		fmt.Printf("test.Runner.Share.Committee[%d].GetPublicKey(): %v\n", i, hex.EncodeToString(test.Runner.Share.Committee[i].GetPublicKey()))
	}

	share := &beaconprotocol.Share{
		NodeID:    1,
		PublicKey: keysSet.ValidatorPK,
		Committee: map[spectypes.OperatorID]*beaconprotocol.Node{
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
		OperatorIds: []uint64{1, 2, 3, 4},
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
		SyncRateLimit:              time.Second * 5,
		SignatureCollectionTimeout: time.Second * 5,
		ReadMode:                   false,
		FullNode:                   false,
		DutyRoles:                  []spectypes.BeaconRole{attesterRoleType},
	})

	qbftCtrl := v.(*validator.Validator).Ibfts()[attesterRoleType].(*controller.Controller)
	qbftCtrl.State = controller.Ready
	go qbftCtrl.StartQueueConsumer(qbftCtrl.MessageHandler)
	require.NoError(t, qbftCtrl.Init())
	go v.StartDuty(test.Duty)

	for _, msg := range test.Messages {
		require.NoError(t, v.ProcessMsg(msg))
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
			Round:                           currentInstance.State().GetRound(),
			Height:                          currentInstance.State().GetHeight(),
			LastPreparedRound:               currentInstance.State().GetPreparedRound(),
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

	mappedDecidedValue := &spectypes.ConsensusData{
		Duty: &spectypes.Duty{
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

	resState := specssv.NewDutyExecutionState(3)
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

func convertToSpecContainer(t *testing.T, container msgcont.MessageContainer) *qbft.MsgContainer {
	c := qbft.NewMsgContainer()
	container.AllMessaged(func(round specqbft.Round, msg *specqbft.SignedMessage) {
		signers := append([]spectypes.OperatorID{}, msg.GetSigners()...)

		// TODO need to use one of the message type (spec/protocol)
		ok, err := c.AddIfDoesntExist(&qbft.SignedMessage{
			Signature: msg.Signature,
			Signers:   signers,
			Message: &qbft.Message{
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
