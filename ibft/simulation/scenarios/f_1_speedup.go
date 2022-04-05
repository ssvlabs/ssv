package scenarios

import (
	"fmt"
	"github.com/bloxapp/ssv/protocol/v1/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"sync"
	"time"

	"github.com/bloxapp/ssv/ibft/valcheck"
	"github.com/bloxapp/ssv/storage/collections"
	"go.uber.org/zap"
)

type f1Speedup struct {
	logger     *zap.Logger
	nodes      []controller.Controller
	valueCheck valcheck.ValueCheck
}

// NewF1Speedup returns initialized changeRoundSpeedup scenario
func NewF1Speedup(logger *zap.Logger, valueCheck valcheck.ValueCheck) IScenario {
	return &f1Speedup{
		logger:     logger,
		valueCheck: valueCheck,
	}
}

func (r *f1Speedup) Start(nodes []controller.Controller, _ []collections.Iibft) {
	r.nodes = nodes
	nodeCount := len(nodes)

	if nodeCount < 3 {
		r.logger.Fatal("not enough nodes, min 3")
	}

	// init ibfts
	var wg sync.WaitGroup
	for i := uint64(1); i <= uint64(3); i++ {
		if i <= 2 {
			wg.Add(1)
			go func(node controller.Controller) {
				if err := node.Init(); err != nil {
					fmt.Printf("error initializing ibft")
				}
				wg.Done()
			}(nodes[i-1])
		} else {
			go func(node controller.Controller, index uint64) {
				time.Sleep(time.Second * 13)
				if err := node.Init(); err != nil {
					fmt.Printf("error initializing ibft")
				}
				r.startNode(node, index)
			}(nodes[i-1], i)
		}
	}

	r.logger.Info("waiting for nodes to init")
	wg.Wait()

	r.logger.Info("start instances")
	for i := uint64(1); i <= uint64(nodeCount); i++ {
		wg.Add(1)
		go func(node controller.Controller, index uint64) {
			defer wg.Done()
			r.startNode(node, index)
		}(nodes[i-1], i)
	}

	wg.Wait()
}

func (r *f1Speedup) startNode(node controller.Controller, index uint64) {
	res, err := node.StartInstance(instance.ControllerStartInstanceOptions{
		Logger:     r.logger,
		ValueCheck: r.valueCheck,
		SeqNumber:  1,
		Value:      []byte("value"),
	})
	if err != nil {
		r.logger.Error("instance returned error", zap.Error(err))
	} else if !res.Decided {
		r.logger.Error("instance could not decide")
	} else {
		r.logger.Info("decided with value", zap.String("decided value", string(res.Msg.Message.Value)))
	}
}
