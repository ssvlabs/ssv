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

type changeRoundSpeedup struct {
	logger     *zap.Logger
	nodes      []controller.Controller
	valueCheck valcheck.ValueCheck
}

// NewChangeRoundSpeedup returns initialized changeRoundSpeedup scenario
func NewChangeRoundSpeedup(logger *zap.Logger, valueCheck valcheck.ValueCheck) IScenario {
	return &changeRoundSpeedup{
		logger:     logger,
		valueCheck: valueCheck,
	}
}

func (r *changeRoundSpeedup) Start(nodes []controller.Controller, _ []collections.Iibft) {
	r.nodes = nodes
	nodeCount := len(nodes)

	if nodeCount < 3 {
		r.logger.Fatal("not enough nodes, min 3")
	}

	// init ibfts
	go func(node controller.Controller, index uint64) {
		if err := node.Init(); err != nil {
			fmt.Printf("error initializing ibft")
		}
		r.startNode(node, index)
	}(nodes[0], 1)
	go func(node controller.Controller, index uint64) {
		time.Sleep(time.Second * 13)
		if err := node.Init(); err != nil {
			fmt.Printf("error initializing ibft")
		}
		r.startNode(node, index)
	}(nodes[1], 2)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func(node controller.Controller, index uint64) {
		time.Sleep(time.Second * 60)
		if err := node.Init(); err != nil {
			fmt.Printf("error initializing ibft")
		}
		r.startNode(node, index)
		wg.Done()
	}(nodes[2], 3)

	wg.Wait()
}

func (r *changeRoundSpeedup) startNode(node controller.Controller, index uint64) {
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
