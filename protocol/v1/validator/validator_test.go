package validator

import (
	"bytes"
	"context"
	"encoding/json"
	qbftStorage "github.com/bloxapp/ssv/ibft/storage"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/spec/qbft"
	"github.com/bloxapp/ssv/spec/ssv"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
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
	storage := qbftStorage.New(db, logger, message.RoleTypeAttester.String(), forksprotocol.V0ForkVersion)

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
	ibfts := setupIbfts(&Options{
		Context:                    v.ctx,
		Logger:                     logger,
		IbftStorage:                storage,
		Network:                    network,
		P2pNetwork:                 p2pNet,
		Beacon:                     beacon,
		Share:                      share,
		ForkVersion:                forksprotocol.V0ForkVersion,
		Signer:                     beacon,
		SyncRateLimit:              0,
		SignatureCollectionTimeout: 5,
		ReadMode:                   false,
		FullNode:                   false,
	}, logger)

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

	var runnerCommittee []*types.Operator
	for k, v := range share.Committee {
		runnerCommittee = append(runnerCommittee, &types.Operator{
			OperatorID: types.OperatorID(k),
			PubKey:     v.Pk,
		})
	}

	currentInstance := v.ibfts[message.RoleTypeAttester] // TODO need to find a way to get the instance

	runner := ssv.Runner{
		BeaconRoleType: 1, // TODO need to cast type to runner type
		BeaconNetwork:  ssv.BeaconNetwork(core.PraterNetwork),
		Share: &types.Share{
			OperatorID:      types.OperatorID(share.NodeID),
			ValidatorPubKey: share.PublicKey.Serialize(),
			SharePubKey:     share.Committee[share.NodeID].Pk,
			Committee:       runnerCommittee,
			Quorum:          3,
			PartialQuorum:   2,
			DomainType:      []byte("cHJpbXVzX3Rlc3RuZXQ="),
			Graffiti:        nil,
		},
		State: &ssv.State{
			RunningInstance:                 currentInstance,
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
	mappedState := qbft.State{
		Share:                           nil,
		ID:                              nil,
		Round:                           0,
		Height:                          0,
		LastPreparedRound:               0,
		LastPreparedValue:               nil,
		ProposalAcceptedForCurrentRound: nil,
		Decided:                         false,
		DecidedValue:                    nil,
		ProposeContainer:                nil,
		PrepareContainer:                nil,
		CommitContainer:                 nil,
		RoundChangeContainer:            nil,
	}
	root, err := mappedState.GetRoot()
	require.NoError(t, err)
	require.Equal(t, "0351bb303531bc5858d312928c9577e9ca0104f3d8986a34fce30f2519908b1e", root) // TODO need to change based on the test
}

func jsonToMsgs(t *testing.T, id message.Identifier) []*message.SSVMessage {
	data := []byte("[\n         {\n            \"MsgType\":3,\n            \"MsgID\":\"lI+0RYLOJTNv2xcSLqxk/loa/DkXTOktYBO+ysEWdm3Fp3jIgN1H3n3/9qD4a6QsAQAAAA==\",\n            \"Data\":\"eyJNZXNzYWdlIjp7IlR5cGUiOjAsIkhlaWdodCI6MCwiUGFydGlhbFNpZ25hdHVyZSI6InFXTU1pNUo1dndta1BZWWRHaXltQjQ3TzBxN3A0ekNYdk1NL2lORFd0eEVKYm1IMW1pOEJ6dXV4N0dUbUF2WjVCNldVVXNjeVF2cXRybEtlNUFJeUZ5V1FRQ2k4ZlZWS01UZDNjWGw2RWNaa2FQNWpZYmRwSGdNcUl2L1R5aDNrIiwiU2lnbmluZ1Jvb3QiOiJnVVVjV0xCNXhhK0U2K1M1S1FEVDZjV2pSbWVNdHR3OFMzN3FMSnl6Vmw4PSIsIlNpZ25lcnMiOlsxXX0sIlNpZ25hdHVyZSI6InVZUW1YVE55OVI2UCtsYWwzMW1CdFlkQ3lwcCtYL2M3UmVxVk1mMTQxK3VVYUY2VzVtWlJtQ3VoOXJKT2VJbW9GNTc3VzkxTVpFUUU4dEN0ZldsaXZTbFZwbldaRC9ZUEU5eVpYT3B5ZG1ab1pXNm9Ba2NlY2VZbWJIYVNGQlFTIiwiU2lnbmVycyI6WzFdfQ==\"\n         },\n         {\n            \"MsgType\":3,\n            \"MsgID\":\"lI+0RYLOJTNv2xcSLqxk/loa/DkXTOktYBO+ysEWdm3Fp3jIgN1H3n3/9qD4a6QsAQAAAA==\",\n            \"Data\":\"eyJNZXNzYWdlIjp7IlR5cGUiOjAsIkhlaWdodCI6MCwiUGFydGlhbFNpZ25hdHVyZSI6Img3WW8yUUpDZnA2TURvRkErQjVMMDNOMWsreFRnM0JENko2TkZUaHVSOU1ucXlDUEJMQjl6WUtUMTAzZEZTRU9DYWFMVG42aEEzUXNmMTZCSEErbkhvWGpreVNON2NaU0pEekE2bUxOVnRocUE0NVFFS2ovbHJpWDZNSExzQU5DIiwiU2lnbmluZ1Jvb3QiOiJnVVVjV0xCNXhhK0U2K1M1S1FEVDZjV2pSbWVNdHR3OFMzN3FMSnl6Vmw4PSIsIlNpZ25lcnMiOlsyXX0sIlNpZ25hdHVyZSI6InJCVDdabUIzSk5CUHhMa2F4Wkl4eDFxOXIvbnp1ZTlJZHcvMkZSR2g3YmQ3ZUY4YVkyU3hXdGFGOTVrU3VqNTBBdTNlZkZWMjVtZ0NKZUI5MThYTFdwRzZGVUFZVFRtOEhsQUV3RTIzVjBqVWpOd3JRenNCVGY4elNZUFV6aGhwIiwiU2lnbmVycyI6WzJdfQ==\"\n         },\n         {\n            \"MsgType\":3,\n            \"MsgID\":\"lI+0RYLOJTNv2xcSLqxk/loa/DkXTOktYBO+ysEWdm3Fp3jIgN1H3n3/9qD4a6QsAQAAAA==\",\n            \"Data\":\"eyJNZXNzYWdlIjp7IlR5cGUiOjAsIkhlaWdodCI6MCwiUGFydGlhbFNpZ25hdHVyZSI6InBBUzRuVWozeG4rVjNWZlFsV3VKdVJ6aVNvdmZld3hqRTdManA0Nlg2bjVkT1M1TkhLRnRWT0I1ZTZlZzZ3Rk5DUmF4RnRveTB2d3hkcmJGNkZmTmR3VzVjWDJ4c24vL2FmMmhVdVVKMFRhQmV5ZGY4OUwxM056Y1RabWxxYitkIiwiU2lnbmluZ1Jvb3QiOiJnVVVjV0xCNXhhK0U2K1M1S1FEVDZjV2pSbWVNdHR3OFMzN3FMSnl6Vmw4PSIsIlNpZ25lcnMiOlszXX0sIlNpZ25hdHVyZSI6Imp1MWxBUUo0QU5tVUhPUFppOSswd2V6K21LUFNQMmNEVUphL3VIdi8zZDFHSnhuM0h5aWhhbEI1VjA0YjlnM0FCRTcyTDBSRWtUNGFKd2RhYTNhVGR0aStuUE1wNm5CdklnUm94OVlyL0hPU2ZCekdueEVKc1ZFanFhQWJUZ2JEIiwiU2lnbmVycyI6WzNdfQ==\"\n         },\n         {\n            \"MsgType\":3,\n            \"MsgID\":\"lI+0RYLOJTNv2xcSLqxk/loa/DkXTOktYBO+ysEWdm3Fp3jIgN1H3n3/9qD4a6QsAQAAAA==\",\n            \"Data\":\"eyJNZXNzYWdlIjp7IlR5cGUiOjAsIkhlaWdodCI6MCwiUGFydGlhbFNpZ25hdHVyZSI6ImpQSUQySVFINFNwZGEyODk1TWZQM1lNU0dCNHA1bXV4MkxYTkI0Zi9Pai9PR0VsMkl1V2p6dWdFNmtYWVNjYUpGUDJRREthL0JMaytqdFRveEoyNTQ0ekJibnRxeHF4bVlPTmhiREEyNFIvWmZiUEh0cWRUNjN0enVzSEkwWDdyIiwiU2lnbmluZ1Jvb3QiOiJnVVVjV0xCNXhhK0U2K1M1S1FEVDZjV2pSbWVNdHR3OFMzN3FMSnl6Vmw4PSIsIlNpZ25lcnMiOls0XX0sIlNpZ25hdHVyZSI6ImlzZS9NYmlBLzBHcXhobnkvVWIzUm9Ybmh1WEE0eGsxRElBcFNRelpoVzkwSWxSUkR2YkRFeWY4L1diaXExRFJCZW5oSCtzOC9hUEpiQ0lTMmNHVElreW1vbmRvYUhjR3RrcjhPaUNjUFJROHJGcVJQNUFtb0U3SnJZbmt3ams1IiwiU2lnbmVycyI6WzRdfQ==\"\n         }\n      ]")
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
