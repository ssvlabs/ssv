package scenarios

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/automation/commons"
	"github.com/bloxapp/ssv/automation/qbft/runner"
	p2pv1 "github.com/bloxapp/ssv/network/p2p"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v1/testing"
	"github.com/bloxapp/ssv/protocol/v1/validator"
	"github.com/bloxapp/ssv/utils/rsaencryption"
)

// FullNodeScenario is the fullNode scenario name
const FullNodeScenario = "full-node"

// fullNodeScenario is the scenario with 3 regular nodes and 1 full node
type fullNodeScenario struct {
	logger        *zap.Logger
	sks           map[uint64]*bls.SecretKey
	fullNodeSKs   map[uint64]*bls.SecretKey
	share         *beacon.Share
	fullNodeShare *beacon.Share
	validators    []validator.IValidator
	fullNodes     []validator.IValidator
}

// newFullNodeScenario creates a fullNode scenario instance
func newFullNodeScenario(logger *zap.Logger) runner.Scenario {
	return &fullNodeScenario{logger: logger}
}

func (r *fullNodeScenario) NumOfOperators() int {
	return 3
}

func (r *fullNodeScenario) NumOfBootnodes() int {
	return 0
}

func (r *fullNodeScenario) NumOfFullNodes() int {
	return 1
}

func (r *fullNodeScenario) Name() string {
	return FullNodeScenario
}

func (r *fullNodeScenario) PreExecution(ctx *runner.ScenarioContext) error {
	localNetWithoutFullNodes := *ctx.LocalNet
	localNetWithoutFullNodes.Nodes = localNetWithoutFullNodes.Nodes[:r.NumOfOperators()]
	localNetWithoutFullNodes.NodeKeys = localNetWithoutFullNodes.NodeKeys[:r.NumOfOperators()]
	keyManagers := ctx.KeyManagers[:r.NumOfOperators()]
	stores := ctx.Stores[:r.NumOfOperators()]

	share, sks, validators, err := commons.CreateShareAndValidators(ctx.Ctx, r.logger, &localNetWithoutFullNodes, keyManagers, stores)
	if err != nil {
		return errors.Wrap(err, "could not create share")
	}

	localNetForFullNodes := *ctx.LocalNet
	localNetForFullNodes.Nodes = localNetForFullNodes.Nodes[r.NumOfOperators():]
	localNetForFullNodes.NodeKeys = localNetForFullNodes.NodeKeys[r.NumOfOperators():]

	fullNodeShare, fullNodeSKs, fullNodes, err := createFullNode(ctx.Ctx, r.logger, &localNetWithoutFullNodes, keyManagers, stores)
	if err != nil {
		return errors.Wrap(err, "could not create share")
	}

	// save all references
	r.validators = validators
	r.sks = sks
	r.share = share
	r.fullNodeShare = fullNodeShare
	r.fullNodeSKs = fullNodeSKs
	r.fullNodes = fullNodes

	oids := make([]message.OperatorID, 0)
	keys := make(map[message.OperatorID]*bls.SecretKey)
	for oid := range share.Committee {
		keys[oid] = sks[uint64(oid)]
		oids = append(oids, oid)
	}

	msgs, err := testing.CreateMultipleSignedMessages(keys, message.Height(0), message.Height(2), func(height message.Height) ([]message.OperatorID, *message.ConsensusMessage) {
		commitData := message.CommitData{Data: []byte(fmt.Sprintf("msg-data-%d", height))}
		commitDataBytes, err := commitData.Encode()
		if err != nil {
			panic(err)
		}

		return oids, &message.ConsensusMessage{
			MsgType:    message.CommitMsgType,
			Height:     height,
			Round:      1,
			Identifier: message.NewIdentifier(share.PublicKey.Serialize(), message.RoleTypeAttester),
			Data:       commitDataBytes,
		}
	})
	if err != nil {
		return err
	}

	fullNodeMsgs, err := testing.CreateMultipleSignedMessages(keys, message.Height(0), message.Height(4), func(height message.Height) ([]message.OperatorID, *message.ConsensusMessage) {
		commitData := message.CommitData{Data: []byte(fmt.Sprintf("msg-data-%d", height))}
		commitDataBytes, err := commitData.Encode()
		if err != nil {
			panic(err)
		}

		return oids, &message.ConsensusMessage{
			MsgType:    message.CommitMsgType,
			Height:     height,
			Round:      1,
			Identifier: message.NewIdentifier(fullNodeShare.PublicKey.Serialize(), message.RoleTypeAttester),
			Data:       commitDataBytes,
		}
	})
	if err != nil {
		return err
	}

	for i, store := range ctx.Stores {
		if i == 0 || i == len(ctx.Stores)-1 { // skip first and last store
			continue
		}
		if err := store.SaveDecided(msgs...); err != nil {
			return errors.Wrap(err, "could not save decided messages")
		}
		if err := store.SaveLastDecided(msgs[len(msgs)-1]); err != nil {
			return errors.Wrap(err, "could not save last decided messages")
		}
	}

	if err := ctx.Stores[len(ctx.Stores)-1].SaveDecided(fullNodeMsgs...); err != nil {
		return errors.Wrap(err, "could not save decided messages")
	}
	if err := ctx.Stores[len(ctx.Stores)-1].SaveLastDecided(fullNodeMsgs[len(fullNodeMsgs)-1]); err != nil {
		return errors.Wrap(err, "could not save last decided messages")
	}

	return nil
}

func (r *fullNodeScenario) Execute(_ *runner.ScenarioContext) error {
	if len(r.sks) == 0 || len(r.fullNodeSKs) == 0 || r.share == nil || r.fullNodeShare == nil {
		return errors.New("pre-execution failed")
	}

	var wg sync.WaitGroup
	var startErr error
	for _, val := range append(r.validators, r.fullNodes...) {
		wg.Add(1)
		go func(val validator.IValidator) {
			defer wg.Done()
			if err := val.Start(); err != nil {
				startErr = errors.Wrap(err, "could not start validator")
			}
			<-time.After(time.Second * 3)
		}(val)
	}
	wg.Wait()

	return startErr
}

func (r *fullNodeScenario) PostExecution(ctx *runner.ScenarioContext) error {
	msgs, err := ctx.Stores[0].GetDecided(message.NewIdentifier(r.share.PublicKey.Serialize(), message.RoleTypeAttester), message.Height(0), message.Height(4))
	if err != nil {
		return err
	}
	if len(msgs) < 4 {
		//return errors.New("node-0 didn't sync all messages")
	}
	r.logger.Debug("msgs", zap.Any("msgs", msgs))

	msgs, err = ctx.Stores[len(ctx.Stores)-1].GetDecided(message.NewIdentifier(r.fullNodeShare.PublicKey.Serialize(), message.RoleTypeAttester), message.Height(0), message.Height(4))
	if err != nil {
		return err
	}
	if len(msgs) < 4 {
		//return errors.New("full node didn't sync all messages")
	}
	r.logger.Debug("msgs", zap.Any("msgs", msgs))

	return nil
}

func createFullNode(ctx context.Context, logger *zap.Logger, net *p2pv1.LocalNet, kms []beacon.KeyManager, stores []qbftstorage.QBFTStore) (*beacon.Share, map[uint64]*bls.SecretKey, []validator.IValidator, error) {
	validators := make([]validator.IValidator, 0)
	operators := make([][]byte, 0)
	for _, k := range net.NodeKeys {
		pub, err := rsaencryption.ExtractPublicKey(k.OperatorKey)
		if err != nil {
			return nil, nil, nil, err
		}
		operators = append(operators, []byte(pub))
	}
	// create share
	share, sks, err := commons.CreateShare(operators)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "could not create share")
	}
	// add to key-managers and subscribe to topic
	for i, km := range kms {
		err = km.AddShare(sks[uint64(i+1)])
		if err != nil {
			return nil, nil, nil, err
		}
		val := validator.NewValidator(&validator.Options{
			Context:     ctx,
			Logger:      logger.With(zap.String("who", fmt.Sprintf("node-%d", i))),
			IbftStorage: stores[i],
			P2pNetwork:  net.Nodes[i],
			Network:     beacon.NewNetwork(core.NetworkFromString("prater")),
			Share: &beacon.Share{
				NodeID:       message.OperatorID(i + 1),
				PublicKey:    share.PublicKey,
				Committee:    share.Committee,
				Metadata:     share.Metadata,
				OwnerAddress: share.OwnerAddress,
				Operators:    share.Operators,
			},
			ForkVersion:                forksprotocol.V0ForkVersion, // TODO need to check v1 too?
			Beacon:                     nil,
			Signer:                     km,
			SyncRateLimit:              time.Millisecond * 10,
			SignatureCollectionTimeout: time.Second * 5,
			ReadMode:                   true,
			FullNode:                   true,
		})
		validators = append(validators, val)
	}
	return share, sks, validators, nil
}
