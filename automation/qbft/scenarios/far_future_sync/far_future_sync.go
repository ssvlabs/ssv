package main

import (
	"fmt"
	"github.com/bloxapp/ssv/automation/qbft/scenarios"
	"sync"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/bloxapp/ssv/automation/commons"
	"github.com/bloxapp/ssv/automation/qbft/runner"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/controller"
	ibftinstance "github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v1/validator"
	"github.com/bloxapp/ssv/utils/logex"
)

func main() {
	logger := logex.Build("simulation", zapcore.DebugLevel, nil)
	runner.Start(logger, newFarFutureSyncScenario(logger), scenarios.QBFTScenarioBootstrapper())
}

type farFutureSyncScenario struct {
	logger     *zap.Logger
	sks        map[uint64]*bls.SecretKey
	share      *beacon.Share
	validators []validator.IValidator
}

// newFarFutureSyncScenario creates a farFutureSync scenario instance
func newFarFutureSyncScenario(logger *zap.Logger) runner.Scenario {
	return &farFutureSyncScenario{logger: logger}
}

func (r *farFutureSyncScenario) NumOfOperators() int {
	return 4
}

func (r *farFutureSyncScenario) NumOfBootnodes() int {
	return 0
}

func (r *farFutureSyncScenario) Name() string {
	return "farFutureSync"
}

func (r *farFutureSyncScenario) PreExecution(ctx *runner.ScenarioContext) error {
	share, sks, validators, err := commons.CreateShareAndValidators(ctx.Ctx, r.logger, ctx.LocalNet, ctx.KeyManagers, ctx.Stores)
	if err != nil {
		return errors.Wrap(err, "could not create share")
	}
	// save all references
	r.validators = validators
	r.sks = sks
	r.share = share

	routers := make([]*runner.Router, r.NumOfOperators())

	loggerFactory := func(who string) *zap.Logger {
		logger := zap.L().With(zap.String("who", who))
		return logger
	}

	for i, node := range ctx.LocalNet.Nodes {
		routers[i] = &runner.Router{
			Logger:      loggerFactory(fmt.Sprintf("msgRouter-%d", i)),
			Controllers: r.validators[i].(*validator.Validator).Ibfts(),
		}
		node.UseMessageRouter(routers[i])
	}

	if len(r.sks) == 0 || r.share == nil {
		return errors.New("pre-execution failed")
	}

	var startErr error

	// init ibfts
	var wg sync.WaitGroup
	for i := uint64(1); i < uint64(r.NumOfOperators()); i++ {
		wg.Add(1)
		go func(i uint64) {
			if err := r.initNode(r.validators[i-1], ctx.LocalNet.Nodes[i-1]); err != nil && startErr == nil {
				startErr = err
			}
			wg.Done()
		}(i)
	}

	r.logger.Info("waiting for nodes to init")
	wg.Wait()

	// start several instances one by one
	seqNumber := message.Height(0)
loop:
	for {
		r.logger.Info("started instances")
		for i := uint64(1); i < uint64(r.NumOfOperators()); i++ {
			wg.Add(1)
			go func(node validator.IValidator, index uint64, seqNumber message.Height) {
				if err := r.startNode(node, seqNumber); err != nil {
					r.logger.Error("could not start node", zap.Error(err))
				}
				wg.Done()
			}(r.validators[i-1], i, seqNumber)
		}
		wg.Wait()
		if seqNumber == 25 {
			break loop
		}

		seqNumber++
	}

	return nil
}

func (r *farFutureSyncScenario) Execute(ctx *runner.ScenarioContext) error {
	r.logger.Info("starting node $4")
	if err := r.initNode(r.validators[3], ctx.LocalNet.Nodes[3]); err != nil {
		return err
	}

	for i, ibftc := range r.validators[3].(*validator.Validator).Ibfts() {
		nextSeq, err := ibftc.NextSeqNumber()
		if err != nil {
			r.logger.Error("node #4 could not get state", zap.Int64("index", int64(i)))
			return errors.New("node #4 could not get state")
		}
		r.logger.Info("node #4 synced", zap.Int64("highest decided", int64(nextSeq)-1), zap.Int64("index", int64(i)))
	}
	return nil
}

func (r *farFutureSyncScenario) PostExecution(ctx *runner.ScenarioContext) error {
	i := r.NumOfOperators() - 1
	msgs, err := ctx.Stores[i].GetDecided(message.NewIdentifier(r.share.PublicKey.Serialize(), message.RoleTypeAttester), message.Height(0), message.Height(26))
	if err != nil {
		return err
	}

	if len(msgs) < 26 {
		return fmt.Errorf("node-%d didn't sync all messages", i)
	}

	return nil
}

func (r *farFutureSyncScenario) initNode(val validator.IValidator, net network.P2PNetwork) error {
	if err := net.Subscribe(val.GetShare().PublicKey.Serialize()); err != nil {
		return errors.Wrap(err, "failed to subscribe topic")
	}

	ibftControllers := val.(*validator.Validator).Ibfts()

	for _, ib := range ibftControllers {
		if err := ib.Init(); err != nil {
			if err == controller.ErrAlreadyRunning {
				r.logger.Debug("ibft init is already running")
				continue
			}
			r.logger.Error("could not initialize ibft instance", zap.Error(err))
			return err
		}
	}

	return nil
}

func (r *farFutureSyncScenario) startNode(val validator.IValidator, seqNumber message.Height) error {
	ibftControllers := val.(*validator.Validator).Ibfts()

	for _, ibftc := range ibftControllers {
		res, err := ibftc.StartInstance(ibftinstance.ControllerStartInstanceOptions{
			Logger:    r.logger,
			SeqNumber: seqNumber,
			Value:     []byte("value"),
		})

		if err != nil {
			return err
		} else if !res.Decided {
			return errors.New("instance could not decide")
		} else {
			r.logger.Info("decided with value", zap.String("decided value", string(res.Msg.Message.Data)))
		}
	}

	return nil
}
