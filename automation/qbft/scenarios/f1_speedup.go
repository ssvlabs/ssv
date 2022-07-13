package scenarios

import (
	"fmt"
	"sync"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/automation/commons"
	"github.com/bloxapp/ssv/automation/qbft/runner"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/controller"
	ibftinstance "github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v1/validator"
)

// F1SpeedupScenario is the f1 speedup scenario name
const F1SpeedupScenario = "F1Speedup"

type f1SpeedupScenario struct {
	logger     *zap.Logger
	sks        map[uint64]*bls.SecretKey
	share      *beacon.Share
	validators []validator.IValidator
}

// newF1SpeedupScenario creates a f1Speedup scenario instance
func newF1SpeedupScenario(logger *zap.Logger) runner.Scenario {
	return &f1SpeedupScenario{logger: logger}
}

func (r *f1SpeedupScenario) NumOfOperators() int {
	return 4
}

func (r *f1SpeedupScenario) NumOfBootnodes() int {
	return 0
}

func (r *f1SpeedupScenario) NumOfFullNodes() int {
	return 0
}

func (r *f1SpeedupScenario) Name() string {
	return F1SpeedupScenario
}

func (r *f1SpeedupScenario) PreExecution(ctx *runner.ScenarioContext) error {
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

	return nil
}

func (r *f1SpeedupScenario) Execute(ctx *runner.ScenarioContext) error {
	if len(r.sks) == 0 || r.share == nil {
		return errors.New("pre-execution failed")
	}

	var startErr error

	// init ibfts
	var wg sync.WaitGroup
	for i := uint64(1); i <= uint64(r.NumOfOperators()); i++ {
		if i <= 2 {
			wg.Add(1)
			go func(i uint64) {
				defer wg.Done()
				if err := r.initNode(r.validators[i-1], ctx.LocalNet.Nodes[i-1]); err != nil {
					r.logger.Error("error initializing ibft",
						zap.Uint64("index", i),
						zap.Error(err),
					)
					return
				}
			}(i)
		} else {
			go func(i uint64) {
				time.Sleep(time.Second * 13)
				if err := r.initNode(r.validators[i-1], ctx.LocalNet.Nodes[i-1]); err != nil {
					r.logger.Error("error initializing ibft",
						zap.Uint64("index", i),
						zap.Error(err),
					)
					return
				}
				if err := r.startNode(r.validators[i-1]); err != nil {
					r.logger.Error("error starting ibft",
						zap.Uint64("index", i),
						zap.Error(err),
					)
					return
				}
			}(i)
		}
	}

	r.logger.Info("waiting for nodes to init")
	wg.Wait()

	r.logger.Info("start instances")
	for i := uint64(1); i <= uint64(r.NumOfOperators()); i++ {
		wg.Add(1)
		go func(i uint64) {
			defer wg.Done()
			if err := r.startNode(r.validators[i-1]); err != nil {
				r.logger.Error("error starting ibft",
					zap.Uint64("index", i),
					zap.Error(err),
				)
				return
			}
		}(i)
	}

	wg.Wait()

	return startErr
}

func (r *f1SpeedupScenario) PostExecution(ctx *runner.ScenarioContext) error {
	for i := range ctx.Stores[:2] {
		msgs, err := ctx.Stores[i].GetDecided(message.NewIdentifier(r.share.PublicKey.Serialize(), spectypes.BNRoleAttester), specqbft.Height(0), specqbft.Height(4))
		if err != nil {
			return err
		}
		if len(msgs) < 1 {
			return fmt.Errorf("node-%d didn't sync all messages", i)
		}
	}

	return nil
}

func (r *f1SpeedupScenario) initNode(val validator.IValidator, net network.P2PNetwork) error {
	if err := net.Subscribe(val.GetShare().PublicKey.Serialize()); err != nil {
		return errors.Wrap(err, "failed to subscribe topic")
	}

	ibftc := val.(*validator.Validator).Ibfts()[spectypes.BNRoleAttester]

	if err := ibftc.Init(); err != nil {
		if err == controller.ErrAlreadyRunning {
			r.logger.Debug("ibft init is already running")
			return nil
		}
		r.logger.Error("could not initialize ibft instance", zap.Error(err))
		return err
	}

	return nil
}

func (r *f1SpeedupScenario) startNode(val validator.IValidator) error {
	ibftc := val.(*validator.Validator).Ibfts()[spectypes.BNRoleAttester]

	res, err := ibftc.StartInstance(ibftinstance.ControllerStartInstanceOptions{
		Logger:    r.logger,
		SeqNumber: 1,
		Value:     []byte("value"),
	})

	if err != nil {
		return err
	} else if !res.Decided {
		return errors.New("instance could not decide")
	} else {
		r.logger.Info("decided with value", zap.String("decided value", string(res.Msg.Message.Data)))
	}

	return nil
}
