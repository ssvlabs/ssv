package scenarios

import (
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/valcheck"
	"github.com/bloxapp/ssv/storage/collections"
	validatorstorage "github.com/bloxapp/ssv/validator/storage"
	"go.uber.org/zap"
	"sync"
	"time"
)

type f1MultiRound struct {
	logger     *zap.Logger
	nodes      []ibft.IBFT
	shares     map[uint64]*validatorstorage.Share
	valueCheck valcheck.ValueCheck
}

// NewF1MultiRound returns initialized f1Speedup scenario
func NewF1MultiRound(logger *zap.Logger, valueCheck valcheck.ValueCheck) IScenario {
	return &f1MultiRound{
		logger:     logger,
		valueCheck: valueCheck,
	}
}

func (r *f1MultiRound) Start(nodes []ibft.IBFT, shares map[uint64]*validatorstorage.Share, _ []collections.Iibft) {
	r.nodes = nodes
	r.shares = shares
	//
	wg := sync.WaitGroup{}
	go func(node ibft.IBFT, index uint64) {
		r.nodes[0].Init()
		r.startNode(node, index)
	}(r.nodes[0], 1)
	go func(node ibft.IBFT, index uint64) {
		r.nodes[1].Init()
		time.Sleep(time.Second * 13)
		r.startNode(node, index)
	}(r.nodes[1], 2)
	wg.Add(1)
	go func() {
		time.Sleep(time.Second * 30)
		r.nodes[2].Init()
		r.startNode(r.nodes[2], 3)
		wg.Done()
	}()
	wg.Wait()
}

func (r *f1MultiRound) startNode(node ibft.IBFT, index uint64) {
	res, err := node.StartInstance(ibft.StartOptions{
		Logger:         r.logger,
		ValueCheck:     r.valueCheck,
		SeqNumber:      1,
		Value:          []byte("value"),
		ValidatorShare: r.shares[index],
	})
	if err != nil {
		r.logger.Error("instance returned error", zap.Error(err))
	} else if !res.Decided {
		r.logger.Error("instance could not decide")
	} else {
		r.logger.Info("decided with value", zap.String("decided value", string(res.Msg.Message.Value)))
	}
}
