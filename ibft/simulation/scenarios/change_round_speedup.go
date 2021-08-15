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

type changeRoundSpeedup struct {
	logger     *zap.Logger
	nodes      []ibft.IBFT
	shares     map[uint64]*validatorstorage.Share
	valueCheck valcheck.ValueCheck
}

// NewF1Speedup returns initialized changeRoundSpeedup scenario
func NewChangeRoundSpeedup(logger *zap.Logger, valueCheck valcheck.ValueCheck) IScenario {
	return &changeRoundSpeedup{
		logger:     logger,
		valueCheck: valueCheck,
	}
}

func (r *changeRoundSpeedup) Start(nodes []ibft.IBFT, shares map[uint64]*validatorstorage.Share, _ []collections.Iibft) {
	r.nodes = nodes
	r.shares = shares
	nodeCount := len(nodes)

	if nodeCount < 3 {
		r.logger.Fatal("not enough nodes, min 3")
	}

	// init ibfts
	var wg sync.WaitGroup
	for i := uint64(1); i <= uint64(2); i++ {
		wg.Add(1)
		go func(node ibft.IBFT) {
			node.Init()
			wg.Done()
		}(nodes[i-1])
	}

	r.logger.Info("waiting for nodes to init")
	wg.Wait()
	r.logger.Info("start instances")

	// start node 3 in delay
	go func(node ibft.IBFT, index uint64) {
		time.Sleep(time.Second * 60)
		r.logger.Info("starting node 3")
		node.Init()
		r.startNode(node, index)
	}(nodes[2], 3)

	// start instances
	for i := uint64(1); i <= uint64(2); i++ {
		wg.Add(1)
		go func(node ibft.IBFT, index uint64) {
			defer wg.Done()
			r.startNode(node, index)
		}(nodes[i-1], i)
	}

	wg.Wait()
}

func (r *changeRoundSpeedup) startNode(node ibft.IBFT, index uint64) {
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
