package validator

import (
	"bytes"
	"context"
	"encoding/json"
	qbftStorage "github.com/bloxapp/ssv/ibft/storage"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	qbft2 "github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/controller"
	forksfactory "github.com/bloxapp/ssv/protocol/v1/qbft/controller/forks/factory"
	"github.com/bloxapp/ssv/protocol/v1/qbft/msgqueue"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v1/qbft/strategy/factory"
	"github.com/bloxapp/ssv/spec/qbft"
	"github.com/bloxapp/ssv/spec/ssv"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"go.uber.org/zap"
	"sync"
	"testing"
	"time"

	"github.com/bloxapp/eth2-key-manager/core"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	protocolp2p "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/bloxapp/ssv/spec/types/testingutils"

	"github.com/bloxapp/ssv/utils/logex"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
)

func init() {
	logex.Build("test", zapcore.DebugLevel, nil)
}

func TestIdentifierTest(t *testing.T) {
	node := testingValidator(t, true, 4, []byte{1, 2, 3, 4})
	require.True(t, oneOfIBFTIdentifiers(node, []byte{1, 2, 3, 4}))
	require.False(t, oneOfIBFTIdentifiers(node, []byte{1, 2, 3, 3}))
}

func oneOfIBFTIdentifiers(v *Validator, toMatch []byte) bool {
	for _, i := range v.ibfts {
		if bytes.Equal(i.GetIdentifier(), toMatch) {
			return true
		}
	}
	return false
}

func TestConvertDutyRunner(t *testing.T) {
	logger := logex.GetLogger()

	ctx, cancel := context.WithCancel(context.TODO())
	pi, _ := protocolp2p.GenPeerID()
	p2pNet := protocolp2p.NewMockNetwork(logger, pi, 10)
	beacon := newTestBeacon(t)
	network := beaconprotocol.NewNetwork(core.PraterNetwork)
	keysSet := testingutils.Testing4SharesSet()

	// keysSet.Shares[1].GetPublicKey().Serialize(),

	cfg := basedb.Options{
		Type:   "badger-memory",
		Logger: logger,
		Ctx:    context.Background(),
	}
	db, err := storage.GetStorageFactory(cfg)
	require.NoError(t, err)
	ibftStorage := qbftStorage.New(db, logger, message.RoleTypeAttester.String(), forksprotocol.V0ForkVersion)

	share := &beaconprotocol.Share{
		NodeID:    1,
		PublicKey: keysSet.PK,
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

	v := &Validator{
		ctx:         ctx,
		cancelCtx:   cancel,
		logger:      logger,
		network:     network,
		p2pNetwork:  p2pNet,
		beacon:      beacon,
		Share:       share,
		signer:      beacon,
		ibfts:       nil,
		readMode:    false,
		saveHistory: false,
	}

	attestCtrl := newQbftController(t, ctx, logger, message.RoleTypeAttester, ibftStorage, network, p2pNet, beacon, share, forksprotocol.V0ForkVersion, beacon)
	ibfts := make(map[message.RoleType]controller.IController)
	ibfts[message.RoleTypeAttester] = attestCtrl
	v.ibfts = ibfts

	var dutyPk [48]byte
	copy(dutyPk[:], keysSet.PK.Serialize())

	duty := &beaconprotocol.Duty{
		Type:                    message.RoleTypeAttester,
		PubKey:                  dutyPk,
		Slot:                    12,
		ValidatorIndex:          1,
		CommitteeIndex:          3,
		CommitteeLength:         128,
		CommitteesAtSlot:        36,
		ValidatorCommitteeIndex: 11,
	}
	v.ExecuteDuty(12, duty)

	for _, msg := range jsonToMsgs(t, message.NewIdentifier(keysSet.PK.Serialize(), message.RoleTypeAttester)) {
		require.NoError(t, v.ProcessMsg(msg))
	}

	time.Sleep(time.Second * 2)

	// ------------- OUTPUT
	var mappedCommittee []*types.Operator
	for k, v := range share.Committee {
		mappedCommittee = append(mappedCommittee, &types.Operator{
			OperatorID: types.OperatorID(k),
			PubKey:     v.Pk,
		})
	}

	qbftCtrl := v.ibfts[message.RoleTypeAttester]
	currentInstance := qbftCtrl.GetCurrentInstance()
	mappedShare := &types.Share{
		OperatorID:      types.OperatorID(v.Share.NodeID),
		ValidatorPubKey: v.Share.PublicKey.Serialize(),
		SharePubKey:     v.Share.Committee[v.Share.NodeID].Pk,
		Committee:       mappedCommittee,
		Quorum:          3,
		PartialQuorum:   2,
		DomainType:      []byte("cHJpbXVzX3Rlc3RuZXQ="),
		Graffiti:        nil,
	}
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
			Share:                           mappedShare,
			ID:                              qbftCtrl.GetIdentifier(),
			Round:                           qbft.Round(currentInstance.State().GetRound()),
			Height:                          qbft.Height(currentInstance.State().GetHeight()),
			LastPreparedRound:               qbft.Round(currentInstance.State().GetPreparedRound()),
			LastPreparedValue:               currentInstance.State().GetPreparedValue(),
			ProposalAcceptedForCurrentRound: nil,
			Decided:                         decided != nil && decided.Message.Height == currentInstance.State().GetHeight(), // TODO might need to add this flag to qbftCtrl
			DecidedValue:                    decidedValue,                                                                    // TODO allow a way to get it
			ProposeContainer:                currentInstance,
			PrepareContainer:                nil,
			CommitContainer:                 nil,
			RoundChangeContainer:            nil,
		}
		//mappedInstance.StartValue =
	}

	runner := ssv.Runner{
		BeaconRoleType: 1, // TODO need to cast type to runner type
		BeaconNetwork:  ssv.BeaconNetwork(core.PraterNetwork),
		Share: &types.Share{
			OperatorID:      types.OperatorID(share.NodeID),
			ValidatorPubKey: share.PublicKey.Serialize(),
			SharePubKey:     share.Committee[share.NodeID].Pk,
			Committee:       mappedCommittee,
			Quorum:          3,
			PartialQuorum:   2,
			DomainType:      []byte("cHJpbXVzX3Rlc3RuZXQ="),
			Graffiti:        nil,
		},
		State: &ssv.State{
			RunningInstance:                 mappedInstance,
			DecidedValue:                    nil,
			SignedAttestation:               nil,
			SignedProposal:                  nil,
			SignedAggregate:                 nil,
			SignedSyncCommittee:             nil,
			SignedContributions:             nil,
			SelectionProofPartialSig:        nil,
			RandaoPartialSig:                nil,
			ContributionProofs:              nil,
			ContributionSubCommitteeIndexes: nil,
			PostConsensusPartialSig:         nil,
			Finished:                        false,
		},
		CurrentDuty:    nil,
		QBFTController: nil,
	}

	root, err := runner.GetRoot()
	require.NoError(t, err)
	require.Equal(t, "0351bb303531bc5858d312928c9577e9ca0104f3d8986a34fce30f2519908b1e", root) // TODO need to change based on the test
}

func jsonToMsgs(t *testing.T, id message.Identifier) []*message.SSVMessage {
	data := []byte("[\n{\n\"MsgType\":3,\n\"MsgID\":\"lI+0RYLOJTNv2xcSLqxk/loa/DkXTOktYBO+ysEWdm3Fp3jIgN1H3n3/9qD4a6QsAQAAAA==\",\n\"Data\":\"eyJNZXNzYWdlIjp7IlR5cGUiOjAsIkhlaWdodCI6MCwiUGFydGlhbFNpZ25hdHVyZSI6InFXTU1pNUo1dndta1BZWWRHaXltQjQ3TzBxN3A0ekNYdk1NL2lORFd0eEVKYm1IMW1pOEJ6dXV4N0dUbUF2WjVCNldVVXNjeVF2cXRybEtlNUFJeUZ5V1FRQ2k4ZlZWS01UZDNjWGw2RWNaa2FQNWpZYmRwSGdNcUl2L1R5aDNrIiwiU2lnbmluZ1Jvb3QiOiJnVVVjV0xCNXhhK0U2K1M1S1FEVDZjV2pSbWVNdHR3OFMzN3FMSnl6Vmw4PSIsIlNpZ25lcnMiOlsxXX0sIlNpZ25hdHVyZSI6InVZUW1YVE55OVI2UCtsYWwzMW1CdFlkQ3lwcCtYL2M3UmVxVk1mMTQxK3VVYUY2VzVtWlJtQ3VoOXJKT2VJbW9GNTc3VzkxTVpFUUU4dEN0ZldsaXZTbFZwbldaRC9ZUEU5eVpYT3B5ZG1ab1pXNm9Ba2NlY2VZbWJIYVNGQlFTIiwiU2lnbmVycyI6WzFdfQ==\"\n},\n{\n\"MsgType\":3,\n\"MsgID\":\"lI+0RYLOJTNv2xcSLqxk/loa/DkXTOktYBO+ysEWdm3Fp3jIgN1H3n3/9qD4a6QsAQAAAA==\",\n\"Data\":\"eyJNZXNzYWdlIjp7IlR5cGUiOjAsIkhlaWdodCI6MCwiUGFydGlhbFNpZ25hdHVyZSI6Img3WW8yUUpDZnA2TURvRkErQjVMMDNOMWsreFRnM0JENko2TkZUaHVSOU1ucXlDUEJMQjl6WUtUMTAzZEZTRU9DYWFMVG42aEEzUXNmMTZCSEErbkhvWGpreVNON2NaU0pEekE2bUxOVnRocUE0NVFFS2ovbHJpWDZNSExzQU5DIiwiU2lnbmluZ1Jvb3QiOiJnVVVjV0xCNXhhK0U2K1M1S1FEVDZjV2pSbWVNdHR3OFMzN3FMSnl6Vmw4PSIsIlNpZ25lcnMiOlsyXX0sIlNpZ25hdHVyZSI6InJCVDdabUIzSk5CUHhMa2F4Wkl4eDFxOXIvbnp1ZTlJZHcvMkZSR2g3YmQ3ZUY4YVkyU3hXdGFGOTVrU3VqNTBBdTNlZkZWMjVtZ0NKZUI5MThYTFdwRzZGVUFZVFRtOEhsQUV3RTIzVjBqVWpOd3JRenNCVGY4elNZUFV6aGhwIiwiU2lnbmVycyI6WzJdfQ==\"\n},\n{\n\"MsgType\":3,\n\"MsgID\":\"lI+0RYLOJTNv2xcSLqxk/loa/DkXTOktYBO+ysEWdm3Fp3jIgN1H3n3/9qD4a6QsAQAAAA==\",\n\"Data\":\"eyJNZXNzYWdlIjp7IlR5cGUiOjAsIkhlaWdodCI6MCwiUGFydGlhbFNpZ25hdHVyZSI6InBBUzRuVWozeG4rVjNWZlFsV3VKdVJ6aVNvdmZld3hqRTdManA0Nlg2bjVkT1M1TkhLRnRWT0I1ZTZlZzZ3Rk5DUmF4RnRveTB2d3hkcmJGNkZmTmR3VzVjWDJ4c24vL2FmMmhVdVVKMFRhQmV5ZGY4OUwxM056Y1RabWxxYitkIiwiU2lnbmluZ1Jvb3QiOiJnVVVjV0xCNXhhK0U2K1M1S1FEVDZjV2pSbWVNdHR3OFMzN3FMSnl6Vmw4PSIsIlNpZ25lcnMiOlszXX0sIlNpZ25hdHVyZSI6Imp1MWxBUUo0QU5tVUhPUFppOSswd2V6K21LUFNQMmNEVUphL3VIdi8zZDFHSnhuM0h5aWhhbEI1VjA0YjlnM0FCRTcyTDBSRWtUNGFKd2RhYTNhVGR0aStuUE1wNm5CdklnUm94OVlyL0hPU2ZCekdueEVKc1ZFanFhQWJUZ2JEIiwiU2lnbmVycyI6WzNdfQ==\"\n},\n{\n\"MsgType\":3,\n\"MsgID\":\"lI+0RYLOJTNv2xcSLqxk/loa/DkXTOktYBO+ysEWdm3Fp3jIgN1H3n3/9qD4a6QsAQAAAA==\",\n\"Data\":\"eyJNZXNzYWdlIjp7IlR5cGUiOjAsIkhlaWdodCI6MCwiUGFydGlhbFNpZ25hdHVyZSI6ImpQSUQySVFINFNwZGEyODk1TWZQM1lNU0dCNHA1bXV4MkxYTkI0Zi9Pai9PR0VsMkl1V2p6dWdFNmtYWVNjYUpGUDJRREthL0JMaytqdFRveEoyNTQ0ekJibnRxeHF4bVlPTmhiREEyNFIvWmZiUEh0cWRUNjN0enVzSEkwWDdyIiwiU2lnbmluZ1Jvb3QiOiJnVVVjV0xCNXhhK0U2K1M1S1FEVDZjV2pSbWVNdHR3OFMzN3FMSnl6Vmw4PSIsIlNpZ25lcnMiOls0XX0sIlNpZ25hdHVyZSI6ImlzZS9NYmlBLzBHcXhobnkvVWIzUm9Ybmh1WEE0eGsxRElBcFNRelpoVzkwSWxSUkR2YkRFeWY4L1diaXExRFJCZW5oSCtzOC9hUEpiQ0lTMmNHVElreW1vbmRvYUhjR3RrcjhPaUNjUFJROHJGcVJQNUFtb0U3SnJZbmt3ams1IiwiU2lnbmVycyI6WzRdfQ==\"\n}\n]")
	var msgs []*message.TestSSVMessage
	require.NoError(t, json.Unmarshal(data, &msgs))

	var res []*message.SSVMessage
	for _, msg := range msgs {
		sm := &message.SSVMessage{
			MsgType: msg.MsgType,
			ID:      id,
			Data:    msg.Data,
		}
		res = append(res, sm)
	}
	return res
}

func newQbftController(t *testing.T, ctx context.Context, logger *zap.Logger, roleType message.RoleType, ibftStorage qbftstorage.QBFTStore, network beaconprotocol.Network, net protocolp2p.MockNetwork, beacon *testBeacon, share *beaconprotocol.Share, version forksprotocol.ForkVersion, b *testBeacon) *controller.Controller {
	logger = logger.With(zap.String("role", roleType.String()), zap.Bool("read mode", false))
	identifier := message.NewIdentifier(share.PublicKey.Serialize(), roleType)
	fork := forksfactory.NewFork(version)

	q, err := msgqueue.New(
		logger.With(zap.String("who", "msg_q")),
		msgqueue.WithIndexers( /*msgqueue.DefaultMsgIndexer(), */ msgqueue.SignedMsgIndexer(), msgqueue.DecidedMsgIndexer(), msgqueue.SignedPostConsensusMsgIndexer()),
	)
	require.NoError(t, err)

	ctrl := &controller.Controller{
		Ctx:                ctx,
		InstanceStorage:    ibftStorage,
		ChangeRoundStorage: ibftStorage,
		Logger:             logger,
		Network:            net,
		InstanceConfig:     qbft2.DefaultConsensusParams(),
		ValidatorShare:     share,
		Identifier:         identifier,
		Fork:               fork,
		Beacon:             beacon,
		Signer:             beacon,
		SignatureState:     controller.SignatureState{SignatureCollectionTimeout: time.Second * 5},

		SyncRateLimit: time.Second * 5,

		Q: q,

		CurrentInstanceLock: &sync.RWMutex{},
		ForkLock:            &sync.Mutex{},
	}

	ctrl.DecidedFactory = factory.NewDecidedFactory(logger, ctrl.GetNodeMode(), ibftStorage, net)
	ctrl.DecidedStrategy = ctrl.DecidedFactory.GetStrategy()

	// set flags
	ctrl.State = controller.Ready
	return ctrl
}
