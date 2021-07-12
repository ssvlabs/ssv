package scenarios

import (
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/valcheck"
	validatorstorage "github.com/bloxapp/ssv/validator/storage"
	"go.uber.org/zap"
	"sync"
	"time"
)

type f1Speedup struct {
	logger     *zap.Logger
	nodes      []ibft.IBFT
	shares     map[uint64]*validatorstorage.Share
	valueCheck valcheck.ValueCheck
}

// NewF1Speedup returns initialized f1Speedup scenario
func NewF1Speedup(logger *zap.Logger, valueCheck valcheck.ValueCheck) IScenario {
	return &f1Speedup{
		logger:     logger,
		valueCheck: valueCheck,
	}
}

func (r *f1Speedup) Start(nodes []ibft.IBFT, shares map[uint64]*validatorstorage.Share) {
	r.nodes = nodes
	r.shares = shares
	nodeCount := len(nodes)

	if nodeCount < 3 {
		r.logger.Fatal("not enough nodes, min 3")
	}

	// init ibfts
	var wg sync.WaitGroup
	for i := uint64(1); i <= uint64(3); i++ {
		if i <= 2 {
			wg.Add(1)
			go func(node ibft.IBFT) {
				node.Init()
				wg.Done()
			}(nodes[i-1])
		} else {
			go func(node ibft.IBFT, index uint64) {
				time.Sleep(time.Second * 10)
				node.Init()
				r.startNode(node, index)
			}(nodes[i-1], i)
		}
	}

	r.logger.Info("waiting for nodes to init")
	wg.Wait()

	r.logger.Info("start instances")
	for i := uint64(1); i <= uint64(nodeCount); i++ {
		wg.Add(1)
		go func(node ibft.IBFT, index uint64) {
			defer wg.Done()
			r.startNode(node, index)
		}(nodes[i-1], i)
	}

	wg.Wait()
}

func (r *f1Speedup) startNode(node ibft.IBFT, index uint64) {
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
