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

type f1MultiRound struct {
	logger     *zap.Logger
	nodes      []ibft.IController
	valueCheck valcheck.ValueCheck
}

// NewF1MultiRound returns initialized f1Speedup scenario
func NewF1MultiRound(logger *zap.Logger, valueCheck valcheck.ValueCheck) IScenario {
	return &f1MultiRound{
		logger:     logger,
		valueCheck: valueCheck,
	}
}

func (r *f1MultiRound) Start(nodes []ibft.IController, _ []qbftstorage.QBFTStore) {
	r.nodes = nodes
	wg := sync.WaitGroup{}
	go func(node ibft.IController, index uint64) {
		if err := r.nodes[0].Init(); err != nil {
			fmt.Printf("error initializing ibft")
		}
		r.startNode(node, index)
	}(r.nodes[0], 1)
	go func(node ibft.IController, index uint64) {
		if err := r.nodes[1].Init(); err != nil {
			fmt.Printf("error initializing ibft")
		}
		time.Sleep(time.Second * 13)
		r.startNode(node, index)
	}(r.nodes[1], 2)
	wg.Add(1)
	go func() {
		time.Sleep(time.Second * 30)
		if err := r.nodes[2].Init(); err != nil {
			fmt.Printf("error initializing ibft")
		}
		r.startNode(r.nodes[2], 3)
		wg.Done()
	}()
	wg.Wait()
}

func (r *f1MultiRound) startNode(node ibft.IController, index uint64) {
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
