package scenarios

import (
	"fmt"
	"sync"
	"time"

	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/valcheck"
	"github.com/bloxapp/ssv/storage/collections"
	"go.uber.org/zap"
)

type f1MultiRound struct {
	logger     *zap.Logger
	nodes      []ibft.Controller
	valueCheck valcheck.ValueCheck
}

// NewF1MultiRound returns initialized f1Speedup scenario
func NewF1MultiRound(logger *zap.Logger, valueCheck valcheck.ValueCheck) IScenario {
	return &f1MultiRound{
		logger:     logger,
		valueCheck: valueCheck,
	}
}

func (r *f1MultiRound) Start(nodes []ibft.Controller, _ []collections.Iibft) {
	r.nodes = nodes
	wg := sync.WaitGroup{}
	go func(node ibft.Controller, index uint64) {
		if err := r.nodes[0].Init(); err != nil {
			fmt.Printf("error initializing ibft")
		}
		r.startNode(node, index)
	}(r.nodes[0], 1)
	go func(node ibft.Controller, index uint64) {
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

func (r *f1MultiRound) startNode(node ibft.Controller, index uint64) {
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
}
