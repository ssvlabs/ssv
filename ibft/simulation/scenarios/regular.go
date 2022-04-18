package scenarios

import (
	"fmt"
	"sync"

	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/valcheck"
	"github.com/bloxapp/ssv/storage/collections"
	"go.uber.org/zap"
)

type regular struct {
	logger     *zap.Logger
	nodes      []ibft.Controller
	valueCheck valcheck.ValueCheck
}

// NewRegularScenario returns initialized regular scenario
func NewRegularScenario(logger *zap.Logger, valueCheck valcheck.ValueCheck) IScenario {
	return &regular{
		logger:     logger,
		valueCheck: valueCheck,
	}
}

func (r *regular) Start(nodes []ibft.Controller, _ []collections.Iibft) {
	r.nodes = nodes
	nodeCount := len(nodes)

	// init ibfts
	var wg sync.WaitGroup
	for i := uint64(1); i <= uint64(nodeCount); i++ {
		wg.Add(1)
		go func(node ibft.Controller) {
			if err := node.Init(); err != nil {
				fmt.Printf("error initializing ibft")
			}
			wg.Done()
		}(nodes[i-1])
	}

	r.logger.Info("waiting for nodes to init")
	wg.Wait()

	r.logger.Info("start instances")
	for i := uint64(1); i <= uint64(nodeCount); i++ {
		wg.Add(1)
		go func(node ibft.Controller, index uint64) {
			defer wg.Done()
			res, err := node.StartInstance(ibft.ControllerStartInstanceOptions{
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
		}(nodes[i-1], i)
	}

	wg.Wait()
}
