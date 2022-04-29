package scenarios

import (
	"fmt"
	"sync"
	"time"

	"github.com/bloxapp/ssv/automation/commons"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/controller"
	ibftinstance "github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v1/validator"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// f1MultiRoundScenario is the scenario when new nodes are created with a delay after other nodes already started.
type f1MultiRoundScenario struct {
	logger     *zap.Logger
	sks        map[uint64]*bls.SecretKey
	share      *beacon.Share
	validators []validator.IValidator
}

// NewF1MultiRoundScenario creates a f1MultiRound scenario instance
func NewF1MultiRoundScenario(logger *zap.Logger) Scenario {
	return &f1MultiRoundScenario{logger: logger}
}

func (r *f1MultiRoundScenario) NumOfOperators() int {
	return 4
}

func (r *f1MultiRoundScenario) NumOfExporters() int {
	return 0
}

func (r *f1MultiRoundScenario) Name() string {
	return "f1MultiRound"
}

func (r *f1MultiRoundScenario) PreExecution(ctx *ScenarioContext) error {
	share, sks, validators, err := commons.CreateShareAndValidators(ctx.Ctx, r.logger, ctx.LocalNet, ctx.KeyManagers, ctx.Stores)
	if err != nil {
		return errors.Wrap(err, "could not create share")
	}
	// save all references
	r.validators = validators
	r.sks = sks
	r.share = share

	routers := make([]*router, r.NumOfOperators())

	loggerFactory := func(who string) *zap.Logger {
		logger := zap.L().With(zap.String("who", who))
		return logger
	}

	for i, node := range ctx.LocalNet.Nodes {
		routers[i] = &router{
			logger:      loggerFactory(fmt.Sprintf("msgRouter-%d", i)),
			controllers: r.validators[i].(*validator.Validator).Ibfts(),
		}
		node.UseMessageRouter(routers[i])
	}

	return nil
}

func (r *f1MultiRoundScenario) Execute(ctx *ScenarioContext) error {
	if len(r.sks) == 0 || r.share == nil {
		return errors.New("pre-execution failed")
	}

	var wg sync.WaitGroup
	var startErr error

	go func(val validator.IValidator, net network.P2PNetwork) {
		time.Sleep(time.Second * 13)
		r.startNode(val, net)
	}(r.validators[0], ctx.LocalNet.Nodes[0])

	go func(val validator.IValidator, net network.P2PNetwork) {
		time.Sleep(time.Second * 13)
		r.startNode(val, net)
	}(r.validators[1], ctx.LocalNet.Nodes[1])

	wg.Add(1)
	go func(val validator.IValidator, net network.P2PNetwork) {
		defer wg.Done()
		time.Sleep(time.Second * 30)
		r.startNode(val, net)
	}(r.validators[2], ctx.LocalNet.Nodes[2])

	wg.Wait()

	return startErr
}

func (r *f1MultiRoundScenario) PostExecution(ctx *ScenarioContext) error {
	for i := range ctx.Stores {
		msgs, err := ctx.Stores[i].GetDecided(message.NewIdentifier(r.share.PublicKey.Serialize(), message.RoleTypeAttester), message.Height(0), message.Height(0))
		if err != nil {
			return err
		}
		if len(msgs) < 3 {
			return fmt.Errorf("node-%d didn't sync all messages", i)
		}
	}

	return nil
}

func (r *f1MultiRoundScenario) startNode(val validator.IValidator, net network.P2PNetwork) {
	if err := net.Subscribe(val.GetShare().PublicKey.Serialize()); err != nil {
		r.logger.Error("failed to subscribe topic")
		return
	}

	ibftControllers := val.(*validator.Validator).Ibfts()

	for _, ibftc := range ibftControllers {
		if err := ibftc.Init(); err != nil {
			if err == controller.ErrAlreadyRunning {
				r.logger.Debug("ibft init is already running")
				return
			}
			r.logger.Error("could not initialize ibft instance", zap.Error(err))
			return
		}

		res, err := ibftc.StartInstance(ibftinstance.ControllerStartInstanceOptions{
			Logger:    r.logger,
			SeqNumber: 1,
			Value:     []byte("value"),
		})

		if err != nil {
			r.logger.Error("instance returned error", zap.Error(err))
			return
		} else if !res.Decided {
			r.logger.Error("instance could not decide")
			return
		} else {
			r.logger.Info("decided with value", zap.String("decided value", string(res.Msg.Message.Data)))
		}
	}
}
