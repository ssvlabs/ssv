package scenarios

import (
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/valcheck"
	"github.com/bloxapp/ssv/protocol/v1/message"
	ibft "github.com/bloxapp/ssv/protocol/v1/qbft/controller"
	ibftinstance "github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
)

type syncFailover struct {
	logger     *zap.Logger
	nodes      []ibft.IController
	dbs        []qbftstorage.QBFTStore
	valueCheck valcheck.ValueCheck
}

// SyncFailover returns initialized farFutureSync scenario
func SyncFailover(logger *zap.Logger, valueCheck valcheck.ValueCheck) IScenario {
	return &syncFailover{
		logger:     logger,
		valueCheck: valueCheck,
	}
}

func (sf *syncFailover) Start(nodes []ibft.IController, dbs []qbftstorage.QBFTStore) {
	sf.nodes = nodes
	sf.dbs = dbs
	nodeCount := len(nodes)

	// init ibfts
	var wg sync.WaitGroup
	for i := uint64(1); i < uint64(nodeCount); i++ {
		wg.Add(1)
		go func(node ibft.IController) {
			if err := node.Init(); err != nil {
				fmt.Printf("error initializing ibft")
			}
			wg.Done()
		}(nodes[i-1])
	}

	sf.logger.Info("waiting for nodes to init")
	wg.Wait()

	msgs := map[message.Height]*message.SignedMessage{}
	// start several instances one by one
	seqNumber := message.Height(0)
loop:
	for {
		sf.logger.Info("started instances")
		for i := uint64(1); i < uint64(nodeCount); i++ {
			wg.Add(1)
			go func(node ibft.IController, index uint64) {
				if msg := sf.startNode(node, index, seqNumber); msg != nil {
					msgs[seqNumber] = msg
				}
				wg.Done()
			}(nodes[i-1], i)
		}
		wg.Wait()
		if seqNumber == 10 {
			break loop
		}
		seqNumber++
	}

	// TODO: fix dummy
	dummy := &message.SignedMessage{Message: &message.ConsensusMessage{Identifier: msgs[1].Message.Identifier}}
	// overriding highest decided to make the system dirty
	for i := 0; i < len(dbs)-1; i++ {
		_ = dbs[i].SaveLastDecided(dummy)
	}

	node4 := nodes[3]
	sf.logger.Info("starting node $4")
	if err := node4.Init(); err != nil {
		sf.logger.Debug("error initializing ibft (as planned)", zap.Error(err))
		// fill DBs with correct highest decided and trying to init again
		for i := 0; i < len(dbs)-1; i++ {
			_ = dbs[i].SaveLastDecided(msgs[seqNumber])
		}
		if err = node4.Init(); err != nil {
			sf.logger.Error("failed to reinitialize IBFT", zap.Error(err))
		}
	}

	nextSeq, err := nodes[3].NextSeqNumber()
	if err != nil {
		sf.logger.Error("node #4 could not get state", zap.Error(err))
	} else {
		sf.logger.Info("node #4 synced", zap.Int64("highest decided", int64(nextSeq)-1))
	}

	decides, err := dbs[3].GetDecided(msgs[1].Message.Identifier, 0, 10)
	if err != nil {
		sf.logger.Error("node #4 could not get decided in range", zap.Error(err))
	} else {
		sf.logger.Info("node #4 synced, found decided messages", zap.Int("count", len(decides)))
	}
}

func (sf *syncFailover) startNode(node ibft.IController, index uint64, seqNumber message.Height) *message.SignedMessage {
	res, err := node.StartInstance(ibftinstance.ControllerStartInstanceOptions{
		Logger:    sf.logger,
		SeqNumber: seqNumber,
		Value:     []byte("value"),
	})
	if err != nil {
		sf.logger.Error("instance returned error", zap.Error(err))
		return nil
	} else if !res.Decided {
		sf.logger.Error("instance could not decide")
		return nil
	} else {
		sf.logger.Info("decided with value", zap.String("decided value", string(res.Msg.Message.Data)))
	}

	if err := sf.dbs[index-1].SaveDecided(res.Msg); err != nil {
		sf.logger.Error("could not save decided msg", zap.Uint64("node_id", index), zap.Error(err))
	}
	if err := sf.dbs[index-1].SaveLastDecided(res.Msg); err != nil {
		sf.logger.Error("could not save decided msg", zap.Uint64("node_id", index), zap.Error(err))
	}
	return res.Msg
}
