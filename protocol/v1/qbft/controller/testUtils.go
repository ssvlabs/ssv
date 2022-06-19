package controller

import (
	"context"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	protocolp2p "github.com/bloxapp/ssv/protocol/v1/p2p"
	forksfactory "github.com/bloxapp/ssv/protocol/v1/qbft/controller/forks/factory"
	"github.com/bloxapp/ssv/protocol/v1/qbft/msgqueue"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v1/qbft/strategy/factory"
	"go.uber.org/zap"
	"sync"
)

func newQbftController(ctx context.Context, logger *zap.Logger, roleType message.RoleType, ibftStorage qbftstorage.QBFTStore, network beaconprotocol.Network, net protocolp2p.MockNetwork, beacon *testBeacon, share *beaconprotocol.Share, version forksprotocol.ForkVersion, b *testBeacon) {
	logger := logger.With(zap.String("role", roleType.String()), zap.Bool("read mode", false))
	fork := forksfactory.NewFork(version)

	q, err := msgqueue.New(
		logger.With(zap.String("who", "msg_q")),
		msgqueue.WithIndexers( /*msgqueue.DefaultMsgIndexer(), */ msgqueue.SignedMsgIndexer(), msgqueue.DecidedMsgIndexer(), msgqueue.SignedPostConsensusMsgIndexer()),
	)
	if err != nil {
		// TODO: we should probably stop here, TBD
		logger.Warn("could not setup msg queue properly", zap.Error(err))
	}
	ctrl := &Controller{
		Ctx:                opts.Context,
		InstanceStorage:    opts.Storage,
		ChangeRoundStorage: opts.Storage,
		Logger:             logger,
		Network:            opts.Network,
		InstanceConfig:     opts.InstanceConfig,
		ValidatorShare:     opts.ValidatorShare,
		Identifier:         opts.Identifier,
		Fork:               fork,
		Beacon:             opts.Beacon,
		Signer:             opts.Signer,
		SignatureState:     SignatureState{SignatureCollectionTimeout: opts.SigTimeout},

		SyncRateLimit: opts.SyncRateLimit,

		readMode: opts.ReadMode,
		fullNode: opts.FullNode,

		Q: q,

		CurrentInstanceLock: &sync.RWMutex{},
		ForkLock:            &sync.Mutex{},
	}

	ctrl.DecidedFactory = factory.NewDecidedFactory(logger, ctrl.GetNodeMode(), opts.Storage, opts.Network)
	ctrl.DecidedStrategy = ctrl.DecidedFactory.GetStrategy()

	// set flags
	ctrl.State = NotStarted

}
