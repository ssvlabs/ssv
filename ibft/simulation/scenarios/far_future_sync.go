package scenarios

import (
	"fmt"
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/valcheck"
	"github.com/bloxapp/ssv/storage/collections"
	validatorstorage "github.com/bloxapp/ssv/validator/storage"
	"go.uber.org/zap"
	"sync"
)

type farFutureSync struct {
	logger     *zap.Logger
	nodes      []ibft.Controller
	shares     map[uint64]*validatorstorage.Share
	dbs        []collections.Iibft
	valueCheck valcheck.ValueCheck
}

// FarFutureSync returns initialized farFutureSync scenario
func FarFutureSync(logger *zap.Logger, valueCheck valcheck.ValueCheck) IScenario {
	return &farFutureSync{
		logger:     logger,
		valueCheck: valueCheck,
	}
}

func (r *farFutureSync) Start(nodes []ibft.Controller, shares map[uint64]*validatorstorage.Share, dbs []collections.Iibft) {
	r.nodes = nodes
	r.shares = shares
	r.dbs = dbs
	nodeCount := len(nodes)

	// init ibfts
	var wg sync.WaitGroup
	for i := uint64(1); i < uint64(nodeCount); i++ {
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

	// start several instances one by one
	seqNumber := uint64(0)
loop:
	for {
		r.logger.Info("started instances")
		for i := uint64(1); i < uint64(nodeCount); i++ {
			wg.Add(1)
			go func(node ibft.Controller, index uint64) {
				r.startNode(node, index, seqNumber)
				wg.Done()
			}(nodes[i-1], i)
		}
		wg.Wait()
		if seqNumber == 25 {
			break loop
		}

		seqNumber++
	}

	r.logger.Info("starting node $4")
	if err := nodes[3].Init(); err != nil {
		fmt.Printf("error initializing ibft")
	}

	nextSeq, err := nodes[3].NextSeqNumber()
	if err != nil {
		r.logger.Error("node #4 could not get state")
	} else {
		r.logger.Info("node #4 synced", zap.Uint64("highest decided", nextSeq-1))
	}
}

func (r *farFutureSync) startNode(node ibft.Controller, index uint64, seqNumber uint64) {
	res, err := node.StartInstance(ibft.ControllerStartInstanceOptions{
		Logger:         r.logger,
		ValueCheck:     r.valueCheck,
		SeqNumber:      seqNumber,
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

	if err := r.dbs[index-1].SaveDecided(res.Msg); err != nil {
		r.logger.Error("could not save decided msg", zap.Uint64("node_id", index), zap.Error(err))
	}
	if err := r.dbs[index-1].SaveHighestDecidedInstance(res.Msg); err != nil {
		r.logger.Error("could not save decided msg", zap.Uint64("node_id", index), zap.Error(err))
	}
}
