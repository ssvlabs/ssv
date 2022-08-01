package scenarios

import (
	"fmt"
	"sync"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/automation/commons"
	"github.com/bloxapp/ssv/automation/qbft/runner"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/qbft/controller"
	ibftinstance "github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v1/validator"
)

// SyncFailoverScenario is the sync fail over scenario name
const SyncFailoverScenario = "SyncFailover"

type syncFailoverScenario struct {
	logger     *zap.Logger
	sks        map[uint64]*bls.SecretKey
	share      *beacon.Share
	validators []validator.IValidator
}

// newSyncFailoverScenario creates a syncFailover scenario instance
func newSyncFailoverScenario(logger *zap.Logger) runner.Scenario {
	return &syncFailoverScenario{logger: logger}
}

func (r *syncFailoverScenario) NumOfOperators() int {
	return 4
}

func (r *syncFailoverScenario) NumOfBootnodes() int {
	return 0
}

func (r *syncFailoverScenario) NumOfFullNodes() int {
	return 0
}

func (r *syncFailoverScenario) Name() string {
	return SyncFailoverScenario
}

func (r *syncFailoverScenario) PreExecution(ctx *runner.ScenarioContext) error {
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

	return nil
}

func (r *syncFailoverScenario) Execute(ctx *runner.ScenarioContext) error {
	var wg sync.WaitGroup

	msgs := map[specqbft.Height]*specqbft.Message{}
	// start several instances one by one
	seqNumber := specqbft.Height(0)
loop:
	for {
		r.logger.Info("started instances")
		for i := uint64(1); i < uint64(r.NumOfOperators()); i++ {
			wg.Add(1)
			go func(node validator.IValidator, index uint64, seqNumber specqbft.Height) {
				if msg, err := r.startNode(node, seqNumber); err != nil {
					r.logger.Error("could not start node", zap.Error(err))
				} else {
					msgs[seqNumber] = msg.Message
				}
				wg.Done()
			}(r.validators[i-1], i, seqNumber)
		}
		wg.Wait()
		if seqNumber == 10 {
			break loop
		}

		seqNumber++
	}

	r.logger.Info("starting node $4")
	if err := r.initNode(r.validators[3], &badNetwork{P2PNetwork: ctx.LocalNet.Nodes[3]}); err != nil {
		r.logger.Debug("error initializing ibft (as planned)", zap.Error(err))
		if err := r.initNode(r.validators[3], ctx.LocalNet.Nodes[3]); err != nil {
			r.logger.Error("failed to reinitialize IBFT", zap.Error(err))
		}
	} else {
		return errors.New("init should fail")
	}

	ibftc := r.validators[3].(*validator.Validator).Ibfts()[spectypes.BNRoleAttester]
	nextSeq, err := ibftc.NextSeqNumber()
	if err != nil {
		r.logger.Error("node #4 could not get state", zap.Int64("highest decided", int64(nextSeq)-1))
		return errors.New("node #4 could not get state")
	}
	r.logger.Info("node #4 synced", zap.Int64("highest decided", int64(nextSeq)-1))

	decides, err := ctx.Stores[3].GetDecided(msgs[1].Identifier, 0, nextSeq)
	if err != nil {
		r.logger.Error("node #4 could not get decided in range", zap.Error(err))
		return errors.New("node #4 could not get decided in range")
	} else if len(decides) < int(nextSeq) {
		r.logger.Info("node #4 is not synced, could not find all messages", zap.Int("count", len(decides)))
	} else {
		r.logger.Info("node #4 synced, found decided messages", zap.Int("count", len(decides)))
	}

	return nil
}

func (r *syncFailoverScenario) PostExecution(ctx *runner.ScenarioContext) error {
	i := r.NumOfOperators() - 1
	messageID := spectypes.NewMsgID(r.share.PublicKey.Serialize(), spectypes.BNRoleAttester)
	msgs, err := ctx.Stores[i].GetDecided(messageID[:], specqbft.Height(0), specqbft.Height(11))
	if err != nil {
		return err
	}
	if len(msgs) < 11 {
		return fmt.Errorf("node-%d didn't sync all messages", i)
	}

	return nil
}

func (r *syncFailoverScenario) initNode(val validator.IValidator, net network.P2PNetwork) error {
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

func (r *syncFailoverScenario) startNode(val validator.IValidator, seqNumber specqbft.Height) (*specqbft.SignedMessage, error) {
	ibftc := val.(*validator.Validator).Ibfts()[spectypes.BNRoleAttester]

	res, err := ibftc.StartInstance(ibftinstance.ControllerStartInstanceOptions{
		Logger:    r.logger,
		SeqNumber: seqNumber,
		Value:     []byte("value"),
	})

	if err != nil {
		return nil, err
	} else if !res.Decided {
		return nil, errors.New("instance could not decide")
	} else {
		r.logger.Info("decided with value", zap.String("decided value", string(res.Msg.Message.Data)))
	}

	return res.Msg, nil
}

type badNetwork struct {
	network.P2PNetwork
}

func (b *badNetwork) Subscribe(spectypes.ValidatorPK) error {
	return errors.New("bad network")
}
