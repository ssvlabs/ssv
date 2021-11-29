package scenarios

import (
	"fmt"
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/valcheck"
	"github.com/bloxapp/ssv/storage/collections"
	validatorstorage "github.com/bloxapp/ssv/validator/storage"
	"go.uber.org/zap"
	"sync"
)

type failoverSync struct {
	logger     *zap.Logger
	nodes      []ibft.Controller
	shares     map[uint64]*validatorstorage.Share
	dbs        []collections.Iibft
	valueCheck valcheck.ValueCheck
}

// FailoverSync returns initialized farFutureSync scenario
func FailoverSync(logger *zap.Logger, valueCheck valcheck.ValueCheck) IScenario {
	return &failoverSync{
		logger:     logger,
		valueCheck: valueCheck,
	}
}

func (r *failoverSync) Start(nodes []ibft.Controller, shares map[uint64]*validatorstorage.Share, dbs []collections.Iibft) {
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

	msgs := map[uint64]*proto.SignedMessage{}
	// start several instances one by one
	seqNumber := uint64(0)
loop:
	for {
		r.logger.Info("started instances")
		for i := uint64(1); i < uint64(nodeCount); i++ {
			wg.Add(1)
			go func(node ibft.Controller, index uint64) {
				if msg := r.startNode(node, index, seqNumber); msg != nil {
					msgs[seqNumber] = msg
				}
				wg.Done()
			}(nodes[i-1], i)
		}
		wg.Wait()
		if seqNumber == 25 {
			break loop
		}
		seqNumber++
	}

	dummy := &proto.SignedMessage{Message: &proto.Message{Lambda: msgs[1].Message.Lambda, SeqNumber: uint64(1)}}
	// overriding highest decided to make the system dirty
	for i := 0; i < len(dbs)-1; i++ {
		_ = dbs[i].SaveHighestDecidedInstance(dummy)
	}

	node4 := nodes[3]
	r.logger.Info("starting node $4")
	if err := node4.Init(); err != nil {
		r.logger.Debug("error initializing ibft (as planned)", zap.Error(err))
		// fill DBs with correct highest decided and trying to init again
		for i := 0; i < len(dbs)-1; i++ {
			_ = dbs[i].SaveHighestDecidedInstance(msgs[seqNumber])
		}
		if err = node4.Init(); err != nil {
			r.logger.Error("failed to reinitialize IBFT", zap.Error(err))
		}
	}

	nextSeq, err := nodes[3].NextSeqNumber()
	if err != nil {
		r.logger.Error("node #4 could not get state")
	} else {
		r.logger.Info("node #4 synced", zap.Uint64("highest decided", nextSeq-1))
	}
}

func (r *failoverSync) startNode(node ibft.Controller, index uint64, seqNumber uint64) *proto.SignedMessage {
	res, err := node.StartInstance(ibft.ControllerStartInstanceOptions{
		Logger:         r.logger,
		ValueCheck:     r.valueCheck,
		SeqNumber:      seqNumber,
		Value:          []byte("value"),
		ValidatorShare: r.shares[index],
	})
	if err != nil {
		r.logger.Error("instance returned error", zap.Error(err))
		return nil
	} else if !res.Decided {
		r.logger.Error("instance could not decide")
		return nil
	} else {
		r.logger.Info("decided with value", zap.String("decided value", string(res.Msg.Message.Value)))
	}

	if err := r.dbs[index-1].SaveDecided(res.Msg); err != nil {
		r.logger.Error("could not save decided msg", zap.Uint64("node_id", index), zap.Error(err))
	}
	if err := r.dbs[index-1].SaveHighestDecidedInstance(res.Msg); err != nil {
		r.logger.Error("could not save decided msg", zap.Uint64("node_id", index), zap.Error(err))
	}
	return res.Msg
}
