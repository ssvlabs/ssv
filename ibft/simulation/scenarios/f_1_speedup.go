package scenarios

import (
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/valcheck"
	ibft "github.com/bloxapp/ssv/protocol/v1/qbft/controller"
	ibftinstance "github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
)

type f1Speedup struct {
	logger     *zap.Logger
	nodes      []ibft.IController
	valueCheck valcheck.ValueCheck
}

// NewF1Speedup returns initialized changeRoundSpeedup scenario
func NewF1Speedup(logger *zap.Logger, valueCheck valcheck.ValueCheck) IScenario {
	return &f1Speedup{
		logger:     logger,
		valueCheck: valueCheck,
	}
}

func (r *f1Speedup) Start(nodes []ibft.IController, _ []qbftstorage.QBFTStore) {
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
			go func(node ibft.IController) {
				if err := node.Init(); err != nil {
					fmt.Printf("error initializing ibft")
				}
				wg.Done()
			}(nodes[i-1])
		} else {
			go func(node ibft.IController, index uint64) {
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
		go func(node ibft.IController, index uint64) {
			defer wg.Done()
			r.startNode(node, index)
		}(nodes[i-1], i)
	}

	wg.Wait()
}

func (r *f1Speedup) startNode(node ibft.IController, index uint64) {
	res, err := node.StartInstance(ibftinstance.ControllerStartInstanceOptions{
		Logger:    r.logger,
		SeqNumber: 1,
		Value:     []byte("value"),
	})
	if err != nil {
		r.logger.Error("instance returned error", zap.Error(err))
	} else if !res.Decided {
		r.logger.Error("instance could not decide")
	} else {
		r.logger.Info("decided with value", zap.String("decided value", string(res.Msg.Message.Data)))
	}
}
