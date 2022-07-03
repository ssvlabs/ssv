package scenarios

import (
	"fmt"
	"sync"
	"time"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/automation/commons"
	"github.com/bloxapp/ssv/automation/qbft/runner"
	"github.com/bloxapp/ssv/ibft/conversion"
	"github.com/bloxapp/ssv/network"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v1/testing"
	"github.com/bloxapp/ssv/protocol/v1/validator"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/utils/format"
)

// OnForkV1Scenario is the scenario name for OnForkV1
const OnForkV1Scenario = "OnForkV1"

type onForkV1 struct {
	logger     *zap.Logger
	share      *beacon.Share
	sks        map[uint64]*bls.SecretKey
	validators []validator.IValidator
	msgs       []*message.SignedMessage
}

// newOnForkV1 creates 'on fork v1' scenario
func newOnForkV1(logger *zap.Logger) runner.Scenario {
	return &onForkV1{logger: logger}
}

func (f *onForkV1) NumOfOperators() int {
	return 4
}

func (f *onForkV1) NumOfBootnodes() int {
	return 0
}

func (f *onForkV1) NumOfFullNodes() int {
	return 0
}

func (f *onForkV1) Name() string {
	return OnForkV1Scenario
}

// PreExecution will create messages in v0 format
func (f *onForkV1) PreExecution(ctx *runner.ScenarioContext) error {
	share, sks, validators, err := commons.CreateShareAndValidators(ctx.Ctx, f.logger, ctx.LocalNet, ctx.KeyManagers, ctx.Stores)
	if err != nil {
		return errors.Wrap(err, "could not create share")
	}
	// save all references
	f.validators = validators
	f.sks = sks
	f.share = share

	oids := make([]message.OperatorID, 0)
	keys := make(map[message.OperatorID]*bls.SecretKey)
	for oid := range share.Committee {
		keys[oid] = sks[uint64(oid)]
		oids = append(oids, oid)
	}

	msgs, err := testing.CreateMultipleSignedMessages(keys, message.Height(0), message.Height(4), func(height message.Height) ([]message.OperatorID, *message.ConsensusMessage) {
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
	f.msgs = msgs

	// using old ibft storage to populate db with v0 data
	var v0Stores []collections.Iibft
	for i := range ctx.Stores {
		v0Store := collections.NewIbft(ctx.DBs[i], f.logger.With(zap.String("who", fmt.Sprintf("qbft-store-%d", i+1))), "attestations")
		v0Stores = append(v0Stores, &v0Store)
	}
	for _, msg := range msgs {
		identifier := format.IdentifierFormat(msg.Message.Identifier.GetValidatorPK(),
			msg.Message.Identifier.GetRoleType().String())
		v0Msg, err := conversion.ToSignedMessageV0(msg, []byte(identifier))
		if err != nil {
			return errors.Wrap(err, "could not convert message to v0")
		}
		for i, store := range v0Stores {
			if i == 0 { // skip first store
				continue
			}
			if err := store.SaveDecided(v0Msg); err != nil {
				return errors.Wrap(err, "could not save decided messages")
			}
			// save highest
			err := store.SaveHighestDecidedInstance(v0Msg)
			if err != nil {
				return errors.Wrap(err, "could not save decided messages")
			}
		}
	}

	// setting up routers
	routers := make([]*runner.Router, f.NumOfOperators())
	loggerFactory := func(who string) *zap.Logger {
		logger := zap.L().With(zap.String("who", who))
		return logger
	}

	for i, node := range ctx.LocalNet.Nodes {
		routers[i] = &runner.Router{
			Logger:      loggerFactory(fmt.Sprintf("msgRouter-%d", i)),
			Controllers: f.validators[i].(*validator.Validator).Ibfts(),
		}
		node.UseMessageRouter(routers[i])
	}

	return nil
}

func (f *onForkV1) Execute(ctx *runner.ScenarioContext) error {
	if len(f.sks) == 0 || f.share == nil {
		return errors.New("pre-execution failed")
	}

	var wg sync.WaitGroup
	var startErr error
	for _, val := range f.validators {
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

	if startErr != nil {
		return errors.Wrap(startErr, "could not start validators")
	}

	// running instances pre-fork
	if err := f.startInstances(message.Height(5), message.Height(6)); err != nil {
		return errors.Wrap(err, "could not start instances")
	}

	// forking
	for i := 0; i < f.NumOfOperators(); i++ {
		wg.Add(3)
		go func(node network.P2PNetwork) {
			defer wg.Done()
			if err := node.(forksprotocol.ForkHandler).OnFork(forksprotocol.V1ForkVersion); err != nil {
				f.logger.Panic("could not fork network to v1", zap.Error(err))
			}
		}(ctx.LocalNet.Nodes[i])
		go func(val validator.IValidator) {
			defer wg.Done()
			<-time.After(time.Second)
			if err := val.OnFork(forksprotocol.V1ForkVersion); err != nil {
				f.logger.Panic("could not fork to v1", zap.Error(err))
			}
		}(f.validators[i])
		go func(store qbftstorage.QBFTStore) {
			defer wg.Done()
			<-time.After(time.Second)
			if err := store.(forksprotocol.ForkHandler).OnFork(forksprotocol.V1ForkVersion); err != nil {
				f.logger.Panic("could not fork qbft store to v1", zap.Error(err))
			}
		}(ctx.Stores[i])
	}
	wg.Wait()

	f.logger.Debug("------ after fork, waiting 10 seconds...")
	// waiting 10 sec after fork
	<-time.After(time.Second * 10)
	f.logger.Debug("------ starting instances")

	for i := 0; i < f.NumOfOperators(); i++ {
		peers, err := ctx.LocalNet.Nodes[i].Peers(f.share.PublicKey.Serialize())
		if err != nil {
			return errors.Wrap(err, "could not check peers of topic")
		}
		if len(peers) < f.NumOfOperators()/2 {
			return errors.Errorf("node %d could not find enough peers after fork: %d", i, len(peers))
		}
	}

	// running instances post-fork
	if err := f.startInstances(message.Height(7), message.Height(9)); err != nil {
		return errors.Wrap(err, "could not start instance after fork")
	}

	<-time.After(time.Second * 5)

	return nil
}

func (f *onForkV1) PostExecution(ctx *runner.ScenarioContext) error {
	expectedMsgCount := 9

	for _, store := range ctx.Stores {
		msgs, err := store.GetDecided(message.NewIdentifier(f.share.PublicKey.Serialize(), message.RoleTypeAttester), message.Height(0), message.Height(expectedMsgCount))
		if err != nil {
			return err
		}
		f.logger.Debug("msgs count", zap.Int("len", len(msgs)))
		if len(msgs) < expectedMsgCount {
			return errors.New("node-0 didn't sync all messages")
		}

		msg, err := store.GetLastDecided(message.NewIdentifier(f.share.PublicKey.Serialize(), message.RoleTypeAttester))
		if err != nil {
			return err
		}
		if msg == nil {
			return errors.New("could not find last decided")
		}
		if msg.Message.Height != message.Height(expectedMsgCount) {
			return errors.Errorf("wrong msg height: %d", msg.Message.Height)
		}
	}

	return nil
}

func (f *onForkV1) startInstances(from, to message.Height) error {
	var wg sync.WaitGroup

	h := from

	for h <= to {
		for i := uint64(1); i < uint64(f.NumOfOperators()); i++ {
			wg.Add(1)
			go func(node validator.IValidator, index uint64, seqNumber message.Height) {
				if err := startNode(node, seqNumber, []byte("value"), f.logger); err != nil {
					f.logger.Panic("could not start node", zap.Uint64("node", index-1),
						zap.Uint64("height", uint64(seqNumber)), zap.Error(err))
				}
				wg.Done()
			}(f.validators[i-1], i, h)
		}
		wg.Wait()
		h++
	}
	return nil
}
