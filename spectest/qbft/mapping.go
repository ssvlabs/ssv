package qbft

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	qbft2 "github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v1/qbft/controller/forks/factory"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/leader/constant"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont"
	"github.com/bloxapp/ssv/protocol/v1/qbft/msgqueue"
	"github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v1/qbft/strategy/factory"
	types2 "github.com/bloxapp/ssv/protocol/v1/types"
	"github.com/bloxapp/ssv/protocol/v1/validator"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"sort"
	"sync"
	"testing"
	"time"
)

// BroadcastMessagesGetter interface to support spec tests
type BroadcastMessagesGetter interface {
	GetBroadcastMessages() []types.SSVMessage
	CalledDecidedSyncCnt() int
	SetCalledDecidedSyncCnt(int) // temp solution to pass spec test
}

// NewController returns new qbft controller
func NewController(ctx context.Context, t *testing.T, logger *zap.Logger, identifier types.MessageID, s qbftstorage.QBFTStore, share *beacon.Share, net protcolp2p.MockNetwork, beacon *validator.TestBeacon, version forksprotocol.ForkVersion) *controller.Controller {
	q, err := msgqueue.New(
		logger.With(zap.String("who", "msg_q")),
		msgqueue.WithIndexers(msgqueue.SignedMsgIndexer(), msgqueue.DecidedMsgIndexer(), msgqueue.SignedPostConsensusMsgIndexer()),
	)
	require.NoError(t, err)

	ctrl := &controller.Controller{
		Ctx:                    ctx,
		Logger:                 logger,
		InstanceStorage:        s,
		ChangeRoundStorage:     s,
		Network:                net,
		InstanceConfig:         qbft.DefaultConsensusParams(),
		ValidatorShare:         share,
		Identifier:             identifier[:],
		Fork:                   forksfactory.NewFork(version),
		Beacon:                 beacon,
		KeyManager:             beacon.KeyManager,
		HigherReceivedMessages: make(map[types.OperatorID]qbft2.Height),
		CurrentInstanceLock:    &sync.RWMutex{},
		ForkLock:               &sync.Mutex{},
		SignatureState: controller.SignatureState{
			SignatureCollectionTimeout: time.Second * 5,
		},
		SyncRateLimit: time.Millisecond * 200,
		MinPeers:      0,                     // no need peers
		State:         controller.NotStarted, // mock initialized
		ReadMode:      false,
		Q:             q,
	}

	ctrl.DecidedFactory = factory.NewDecidedFactory(logger, ctrl.GetNodeMode(), s, net)
	ctrl.DecidedStrategy = ctrl.DecidedFactory.GetStrategy()
	return ctrl
}

// GetControllerRoot return controller root by spec
func GetControllerRoot(t *testing.T, c *controller.Controller, storedInstances []instance.Instancer) ([]byte, error) {
	rootStruct := struct {
		Identifier             []byte
		Height                 qbft2.Height
		InstanceRoots          [][]byte
		HigherReceivedMessages map[types.OperatorID]qbft2.Height
		Domain                 types.DomainType
		Share                  *types.Share
	}{
		Identifier:             c.Identifier,
		Height:                 c.GetHeight(),
		InstanceRoots:          make([][]byte, len(storedInstances)),
		HigherReceivedMessages: c.HigherReceivedMessages,
		Domain:                 types2.GetDefaultDomain(), // might need to be dynamic
		Share:                  toSpecShare(c.ValidatorShare),
	}

	instances := make([][]byte, qbft2.HistoricalInstanceCapacity) // spec default history size
	for i, inst := range storedInstances {
		if inst != nil {
			mappedInstance := MapToSpecInstance(t, c.Identifier, inst, c.ValidatorShare)
			r, err := mappedInstance.GetRoot()
			if err != nil {
				return nil, errors.Wrap(err, "failed getting instance root")
			}
			instances[i] = r // keep the same order and size
		}
	}
	rootStruct.InstanceRoots = instances

	marshaledRoot, err := json.Marshal(rootStruct)
	fmt.Println(string(marshaledRoot))
	if err != nil {
		return nil, errors.Wrap(err, "could not encode state")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret[:], nil
}

// NewQbftInstance returns new qbft instance
func NewQbftInstance(logger *zap.Logger, qbftStorage qbftstorage.QBFTStore, net protcolp2p.MockNetwork, beacon *validator.TestBeacon, share *beacon.Share, identifier []byte, forkVersion forksprotocol.ForkVersion) instance.Instancer {
	const height = 0
	fork := forksfactory.NewFork(forkVersion)

	newInstance := instance.NewInstance(&instance.Options{
		Logger:           logger,
		ValidatorShare:   share,
		Network:          net,
		Config:           qbft.DefaultConsensusParams(),
		Identifier:       identifier,
		Height:           height,
		RequireMinPeers:  false,
		Fork:             forkWithValueCheck{fork.InstanceFork()},
		SSVSigner:        beacon.KeyManager,
		ChangeRoundStore: qbftStorage,
	})
	//newInstance.(*instance.Instance).LeaderSelector = roundRobinLeaderSelector{newInstance.GetState(), mappedShare}
	// TODO(nkryuchkov): replace when ready
	newInstance.(*instance.Instance).LeaderSelector = &constant.Constant{
		LeaderIndex: 0,
		OperatorIDs: share.OperatorIds,
	}
	return newInstance
}

// MapToSpecInstance mapping instance to spec instance struct
func MapToSpecInstance(t *testing.T, identifier []byte, qbftInstance instance.Instancer, instanceShare *beacon.Share) *qbft2.Instance {
	mappedInstance := new(qbft2.Instance)
	if qbftInstance != nil {
		preparedValue := qbftInstance.GetState().GetPreparedValue()
		if len(preparedValue) == 0 {
			preparedValue = nil
		}
		round := qbftInstance.GetState().GetRound()

		decided, err := qbftInstance.CommittedAggregatedMsg()
		var decidedValue []byte
		if err == nil && decided != nil && decided.Message != nil {
			cd, err := decided.Message.GetCommitData()
			require.NoError(t, err)
			decidedValue = cd.Data
		}

		mappedInstance.State = &qbft2.State{
			Share:                           toSpecShare(instanceShare),
			ID:                              identifier,
			Round:                           round,
			Height:                          qbftInstance.GetState().GetHeight(),
			LastPreparedRound:               qbftInstance.GetState().GetPreparedRound(),
			LastPreparedValue:               preparedValue,
			ProposalAcceptedForCurrentRound: qbftInstance.GetState().GetProposalAcceptedForCurrentRound(),
			Decided:                         len(decidedValue) != 0,
			DecidedValue:                    decidedValue,
			ProposeContainer:                convertToSpecContainer(t, qbftInstance.Containers()[qbft2.ProposalMsgType]),
			PrepareContainer:                convertToSpecContainer(t, qbftInstance.Containers()[qbft2.PrepareMsgType]),
			CommitContainer:                 convertToSpecContainer(t, qbftInstance.Containers()[qbft2.CommitMsgType]),
			RoundChangeContainer:            convertToSpecContainer(t, qbftInstance.Containers()[qbft2.RoundChangeMsgType]),
		}

		mappedInstance.StartValue = qbftInstance.GetState().GetInputValue()
	}
	return mappedInstance
}

// convertToSpecContainer from ssv container struct to spec struct
func convertToSpecContainer(t *testing.T, container msgcont.MessageContainer) *qbft2.MsgContainer {
	c := qbft2.NewMsgContainer()
	container.AllMessaged(func(round qbft2.Round, msg *qbft2.SignedMessage) {
		ok, err := c.AddIfDoesntExist(&qbft2.SignedMessage{
			Signature: msg.Signature,
			Signers:   msg.Signers,
			Message: &qbft2.Message{
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

// NewQBFTStorage returns qbft storage and db
func NewQBFTStorage(ctx context.Context, t *testing.T, logger *zap.Logger, role string) (basedb.IDb, qbftstorage.QBFTStore) {
	db, err := storage.GetStorageFactory(basedb.Options{
		Type:   "badger-memory",
		Logger: logger,
		Ctx:    ctx,
	})
	require.NoError(t, err)
	return db, qbftstorage.NewQBFTStore(db, logger, role)
}

// toSpecShare convert ssv share to spec share
func toSpecShare(share *beacon.Share) *types.Share {
	specCommittee := make([]*types.Operator, 0)
	for operatorID, node := range share.Committee {
		specCommittee = append(specCommittee, &types.Operator{
			OperatorID: operatorID,
			PubKey:     node.Pk,
		})
	}

	sort.Slice(specCommittee, func(i, j int) bool { // make sure the same order
		return specCommittee[i].OperatorID < specCommittee[j].OperatorID
	})

	return &types.Share{
		OperatorID:      share.NodeID,
		ValidatorPubKey: share.PublicKey.Serialize(),
		SharePubKey:     share.Committee[share.NodeID].Pk,
		Committee:       specCommittee,
		Quorum:          uint64(share.ThresholdSize()),
		PartialQuorum:   uint64(share.PartialThresholdSize()),
		DomainType:      types2.GetDefaultDomain(),
		Graffiti:        nil,
	}
}

// ToMappedShare convert spec share to ssv share
func ToMappedShare(t *testing.T, share *types.Share) (*beacon.Share, *testingutils.TestKeySet) {
	vpk := &bls.PublicKey{}
	require.NoError(t, vpk.Deserialize(share.ValidatorPubKey))

	mappedShare := &beacon.Share{
		NodeID:       share.OperatorID,
		PublicKey:    vpk,
		Committee:    nil, // set in applyCommittee func
		Metadata:     nil,
		OwnerAddress: "",
		Operators:    nil,
		OperatorIds:  nil, // set in applyCommittee func
		Liquidated:   false,
	}
	keySet := applyCommittee(t, mappedShare, share.Committee)

	return mappedShare, keySet
}

// applyCommittee to ssv share from spec share
func applyCommittee(t *testing.T, share *beacon.Share, specCommittee []*types.Operator) *testingutils.TestKeySet {
	var keysSet *testingutils.TestKeySet
	switch len(specCommittee) {
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

	if share.Committee == nil {
		share.Committee = make(map[types.OperatorID]*beacon.Node)
	}
	sort.Slice(specCommittee, func(i, j int) bool { // make sure the same order
		return specCommittee[i].OperatorID < specCommittee[j].OperatorID
	})

	for i := range specCommittee {
		operatorID := types.OperatorID(i) + 1
		pk := keysSet.Shares[operatorID].GetPublicKey().Serialize()
		share.Committee[operatorID] = &beacon.Node{
			IbftID: uint64(i) + 1,
			Pk:     pk,
		}
		share.OperatorIds = append(share.OperatorIds, uint64(i)+1)
	}
	return keysSet
}

// ErrorHandling by spec with error mapping
func ErrorHandling(t *testing.T, expectedError string, lastErr error) {
	if len(expectedError) == 0 {
		require.NoError(t, lastErr)
	} else if mappedLastErr, ok := errorsMap[lastErr.Error()]; ok {
		require.Equal(t, expectedError, mappedLastErr)
	} else {
		require.EqualError(t, lastErr, expectedError)
	}
}
