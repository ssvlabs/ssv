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

type changeRoundSpeedup struct {
	logger     *zap.Logger
	nodes      []ibft.IController
	valueCheck valcheck.ValueCheck
}

// NewChangeRoundSpeedup returns initialized changeRoundSpeedup scenario
func NewChangeRoundSpeedup(logger *zap.Logger, valueCheck valcheck.ValueCheck) IScenario {
	return &changeRoundSpeedup{
		logger:     logger,
		valueCheck: valueCheck,
	}
}

func (r *changeRoundSpeedup) Start(nodes []ibft.IController, _ []qbftstorage.QBFTStore) {
	r.nodes = nodes
	nodeCount := len(nodes)

	if nodeCount < 3 {
		r.logger.Fatal("not enough nodes, min 3")
	}

	// init ibfts
	go func(node ibft.IController, index uint64) {
		if err := node.Init(); err != nil {
			fmt.Printf("error initializing ibft")
		}
		r.startNode(node, index)
	}(nodes[0], 1)
	go func(node ibft.IController, index uint64) {
		time.Sleep(time.Second * 13)
		if err := node.Init(); err != nil {
			fmt.Printf("error initializing ibft")
		}
		r.startNode(node, index)
	}(nodes[1], 2)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func(node ibft.IController, index uint64) {
		time.Sleep(time.Second * 60)
		if err := node.Init(); err != nil {
			fmt.Printf("error initializing ibft")
		}
		r.startNode(node, index)
		wg.Done()
	}(nodes[2], 3)

	wg.Wait()
}

func (r *changeRoundSpeedup) startNode(node ibft.IController, index uint64) {
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
