package main

import (
	"context"
	"encoding/hex"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/controller"
	v0 "github.com/bloxapp/ssv/ibft/controller/forks/v0"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/simulation/scenarios"
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
	"time"
)

/**
Controller Simulator

This simulator is a tool for running many iBFT scenarios for manual testing.
The scenario interface can be overriden with any writen scenario to manually test the Controller inner workings, debug and more.
*/

var (
	nodeCount  = 4
	identifier = []byte("ibft identifier")
	logger     = logex.Build("simulator", zapcore.DebugLevel, nil)
	pkHex      = "88ac8f147d1f25b37aa7fa52cde85d35ced016ae718d2b0ed80ca714a9f4a442bae659111d908e204a0545030c833d95"
	scenario   = scenarios.NewChangeRoundSpeedup(logger, &alwaysTrueValueCheck{})
)

type alwaysTrueValueCheck struct {
}

// Check impl
func (i *alwaysTrueValueCheck) Check(value []byte) error {
	return nil
}

func networking() network.Network {
	ret, err := p2p.New(context.Background(), logger, &p2p.Config{
		DiscoveryType:    "mdns",
		MaxBatchResponse: 10,
		RequestTimeout:   time.Second * 5,
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
			PublicKey: publicKey(),
			ShareKey:  sks[i],
			Committee: nodes,
		}
	}

	return ret
}

func publicKey() *bls.PublicKey {
	_ = bls.Init(bls.BLS12_381)
	byts, err := hex.DecodeString(pkHex)
	if err != nil {
		logger.Fatal("failed to decode pk", zap.Error(err))
	}
	ret := &bls.PublicKey{}
	if err := ret.Deserialize(byts); err != nil {
		logger.Fatal("failed to deserialize pk", zap.Error(err))
	}
	return ret
}

func main() {
	shares := generateShares(uint64(nodeCount))
	pk := publicKey()
	dbs := make([]collections.Iibft, 0)
	logger.Info("pubkey", zap.String("pk", pkHex))

	// generate iBFT nodes
	nodes := make([]ibft.Controller, 0)
	for i := uint64(1); i <= uint64(nodeCount); i++ {
		net := networking()
		if err := net.SubscribeToValidatorNetwork(pk); err != nil {
			logger.Fatal("could not register validator pubsub", zap.Error(err))
		}
		dbs = append(dbs, db())

		node := controller.New(
			beacon.RoleTypeAttester,
			identifier,
			logger.With(zap.Uint64("simulation_node_id", i)),
			dbs[i-1],
			net,
			msgqueue.New(),
			&proto.InstanceConfig{
				RoundChangeDurationSeconds:   3,
				LeaderPreprepareDelaySeconds: 1,
			},
			shares[i],
			v0.New(),
		)
		nodes = append(nodes, node)
	}

	scenario.Start(nodes, shares, dbs)

	logger.Info("finished")
}
