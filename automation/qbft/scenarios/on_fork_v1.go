package scenarios

import (
	"fmt"
	"github.com/bloxapp/ssv/automation/commons"
	"github.com/bloxapp/ssv/automation/qbft/runner"
	"github.com/bloxapp/ssv/ibft/conversion"
	"github.com/bloxapp/ssv/network"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	ibftinstance "github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v1/testing"
	"github.com/bloxapp/ssv/protocol/v1/validator"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/utils/format"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync"
	"time"
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

// NewOnForkV1 creates 'on fork v1' scenario
func NewOnForkV1(logger *zap.Logger) runner.Scenario {
	return &onForkV1{logger: logger}
}

func (f *onForkV1) NumOfOperators() int {
	return 4
}

func (f *onForkV1) NumOfBootnodes() int {
	return 0
}

func (f *onForkV1) Name() string {
	return OnForkV1Scenario
}

func (f *onForkV1) setLogger(l *zap.Logger) {
	f.logger = l
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
	for i := uint64(1); i < uint64(f.NumOfOperators()); i++ {
		wg.Add(3)
		go func(node network.P2PNetwork) {
			defer wg.Done()
			if err := node.(forksprotocol.ForkHandler).OnFork(forksprotocol.V1ForkVersion); err != nil {
				f.logger.Panic("could not fork network to v1", zap.Error(err))
			}
			<-time.After(time.Second * 3)
		}(ctx.LocalNet.Nodes[i-1])
		go func(val validator.IValidator) {
			defer wg.Done()
			<-time.After(time.Millisecond * 10)
			if err := val.OnFork(forksprotocol.V1ForkVersion); err != nil {
				f.logger.Panic("could not fork to v1", zap.Error(err))
			}
			<-time.After(time.Second * 3)
		}(f.validators[i-1])
		go func(store qbftstorage.QBFTStore) {
			defer wg.Done()
			<-time.After(time.Millisecond * 20)
			if err := store.(forksprotocol.ForkHandler).OnFork(forksprotocol.V1ForkVersion); err != nil {
				f.logger.Panic("could not fork qbft store to v1", zap.Error(err))
			}
			<-time.After(time.Second * 3)
		}(ctx.Stores[i-1])
	}
	wg.Wait()

	f.logger.Debug("------ after fork, waiting 10 seconds...")
	// waiting 10 sec after fork
	<-time.After(time.Second * 10)
	f.logger.Debug("------ starting instances")

	// running instances post-fork
	if err := f.startInstances(message.Height(7), message.Height(9)); err != nil {
		return errors.Wrap(err, "could not start instances")
	}

	return nil
}

func (f *onForkV1) PostExecution(ctx *runner.ScenarioContext) error {
	expectedMsgCount := 9
	msgs, err := ctx.Stores[0].GetDecided(message.NewIdentifier(f.share.PublicKey.Serialize(), message.RoleTypeAttester), message.Height(0), message.Height(expectedMsgCount))
	if err != nil {
		return err
	}
	f.logger.Debug("msgs count", zap.Int("len", len(msgs)))
	if len(msgs) < expectedMsgCount {
		return errors.New("node-0 didn't sync all messages")
	}

	msg, err := ctx.Stores[0].GetLastDecided(message.NewIdentifier(f.share.PublicKey.Serialize(), message.RoleTypeAttester))
	if err != nil {
		return err
	}
	if msg == nil {
		return errors.New("could not find last decided")
	}
	if msg.Message.Height != message.Height(expectedMsgCount) {
		return errors.Errorf("wrong msg height: %d", msg.Message.Height)
	}

	return nil
}

func (f *onForkV1) startInstances(from, to message.Height) error {
	var wg sync.WaitGroup

	h := from

	for h <= to {
		f.logger.Info("started instances")
		for i := uint64(1); i < uint64(f.NumOfOperators()); i++ {
			wg.Add(1)
			go func(node validator.IValidator, index uint64, seqNumber message.Height) {
				if err := f.startNode(node, seqNumber); err != nil {
					f.logger.Error("could not start node", zap.Error(err))
				}
				wg.Done()
			}(f.validators[i-1], i, h)
		}
		wg.Wait()
		h++
	}
	return nil
}

func (f *onForkV1) startNode(val validator.IValidator, h message.Height) error {
	ibftControllers := val.(*validator.Validator).Ibfts()

	for _, ibftc := range ibftControllers {
		res, err := ibftc.StartInstance(ibftinstance.ControllerStartInstanceOptions{
			Logger:    f.logger,
			SeqNumber: h,
			Value:     []byte("value"),
		})

		if err != nil {
			return err
		} else if !res.Decided {
			return errors.New("instance could not decide")
		} else {
			f.logger.Info("decided with value", zap.String("decided value", string(res.Msg.Message.Data)))
		}
	}

	return nil
}
