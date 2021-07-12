package main

import (
	"context"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/utils/logex"
	validatorstorage "github.com/bloxapp/ssv/validator/storage"
	"github.com/herumi/bls-eth-go-binary/bls"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"sync"
	"time"
)

type AlwaysTrueValueCheck struct {
}

func (i *AlwaysTrueValueCheck) Check(value []byte) error {
	return nil
}

var (
	NodeCount  = 4
	Identifier = []byte("ibft identifier")
	logger     = logex.Build("simulator", zapcore.InfoLevel)
)

func networking() network.Network {
	ret, err := p2p.New(context.Background(), logger, &p2p.Config{
		DiscoveryType:    "mdns",
		MaxBatchResponse: 10,
		RequestTimeout:   time.Second * 1,
	})
	if err != nil {
		logger.Fatal("failed to create db", zap.Error(err))
	}

	return ret
}

func db() collections.Iibft {
	db, err := storage.GetStorageFactory(basedb.Options{
		Type:   "badger-memory",
		Path:   "",
		Logger: logger,
	})
	if err != nil {
		logger.Fatal("failed to create db", zap.Error(err))
	}

	ret := collections.NewIbft(db, logger, "attestation")

	return &ret
}

func generateShares(cnt uint64) map[uint64]*validatorstorage.Share {
	_ = bls.Init(bls.BLS12_381)
	nodes := make(map[uint64]*proto.Node)
	sks := make(map[uint64]*bls.SecretKey)

	ret := make(map[uint64]*validatorstorage.Share)

	for i := uint64(1); i <= cnt; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()
		nodes[i] = &proto.Node{
			IbftId: i,
			Pk:     sk.GetPublicKey().Serialize(),
		}
		sks[i] = sk
	}

	for i := uint64(1); i <= cnt; i++ {
		ret[i] = &validatorstorage.Share{
			NodeID:    i,
			PublicKey: sks[1].GetPublicKey(),
			ShareKey:  sks[i],
			Committee: nodes,
		}
	}

	return ret
}

func main() {
	shares := generateShares(uint64(NodeCount))

	// generate iBFT nodes
	nodes := make([]ibft.IBFT, 0)
	for i := uint64(1); i <= uint64(NodeCount); i++ {
		net := networking()
		if err := net.SubscribeToValidatorNetwork(shares[i].PublicKey); err != nil {
			logger.Fatal("could not register validator pubsub", zap.Error(err))
		}

		node := ibft.New(
			beacon.RoleAttester,
			Identifier,
			logger,
			db(),
			net,
			msgqueue.New(),
			proto.DefaultConsensusParams(),
			shares[i],
		)
		nodes = append(nodes, node)
	}

	// init ibfts
	var wg sync.WaitGroup
	for i := uint64(1); i <= uint64(NodeCount); i++ {
		wg.Add(1)
		go func(node ibft.IBFT) {
			node.Init()
			wg.Done()
		}(nodes[i-1])
	}

	logger.Info("waiting for nodes to init")
	wg.Wait()

	logger.Info("start instances")
	for i := uint64(1); i <= uint64(NodeCount); i++ {
		wg.Add(1)
		go func(node ibft.IBFT) {
			defer wg.Done()
			res, err := node.StartInstance(ibft.StartOptions{
				Logger:         logger.With(zap.Uint64("node id", i-1)),
				ValueCheck:     &AlwaysTrueValueCheck{},
				SeqNumber:      1,
				Value:          []byte("value"),
				ValidatorShare: shares[i],
			})
			if err != nil {
				logger.Error("instance returned error", zap.Error(err))
			} else if !res.Decided {
				logger.Error("instance could not decide")
			} else {
				logger.Info("decided with value", zap.String("decided value", string(res.Msg.Message.Value)))
			}

		}(nodes[i-1])
	}

	wg.Wait()
	logger.Info("finished")
}
