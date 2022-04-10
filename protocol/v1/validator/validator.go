package validator

import (
	"context"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"go.uber.org/zap"
	"time"

	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/bloxapp/ssv/protocol/v1/qbft/controller"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v1/queue/worker"
	"github.com/bloxapp/ssv/utils/format"
)

type IValidator interface {
	Start()
	ExecuteDuty(slot uint64, duty *beaconprotocol.Duty)
	ProcessMsg(msg *message.SSVMessage) //TODO need to be as separate interface?
	GetShare() *message.Share
}

type Options struct {
	Context                    context.Context
	Logger                     *zap.Logger
	IbftStorage                qbftstorage.QBFTStore
	Network                    beaconprotocol.Network
	P2pNetwork                 p2pprotocol.Network
	Beacon                     beaconprotocol.Beacon
	Share                      *message.Share
	ForkVersion                forksprotocol.ForkVersion
	Signer                     beaconprotocol.Signer
	SyncRateLimit              time.Duration
	SignatureCollectionTimeout time.Duration
	ReadMode                   bool
}

type Validator struct {
	ctx        context.Context
	logger     *zap.Logger
	network    beaconprotocol.Network
	p2pNetwork p2pprotocol.Network
	beacon     beaconprotocol.Beacon
	share      *message.Share
	signer     beaconprotocol.Signer
	worker     *worker.Worker

	ibfts controller.Controllers

	// config
	signatureCollectionTimeout time.Duration

	// flags
	readMode bool
}

func NewValidator(opt *Options) IValidator {
	logger := opt.Logger.With(zap.String("pubKey", opt.Share.PublicKey.SerializeToHexStr())).
		With(zap.Uint64("node_id", uint64(opt.Share.NodeID)))

	workerCfg := &worker.Config{
		Ctx:          opt.Context,
		WorkersCount: 1,   // TODO flag
		Buffer:       100, // TODO flag
	}
	queueWorker := worker.NewWorker(workerCfg)

	ibfts := setupIbfts(opt, logger)

	logger.Debug("new validator instance was created", zap.Strings("operators ids", opt.Share.HashOperators()))
	return &Validator{
		ctx:                        opt.Context,
		logger:                     logger,
		network:                    opt.Network,
		p2pNetwork:                 opt.P2pNetwork,
		beacon:                     opt.Beacon,
		share:                      opt.Share,
		signer:                     opt.Signer,
		ibfts:                      ibfts,
		worker:                     queueWorker,
		signatureCollectionTimeout: opt.SignatureCollectionTimeout,
		readMode:                   opt.ReadMode,
	}
}

func (v *Validator) Start() {
	// start queue workers
	v.worker.AddHandler(v.messageHandler)
}

func (v *Validator) GetShare() *message.Share {
	// TODO need lock?
	return v.share
}

// setupRunners return duty runners map with all the supported duty types
func setupIbfts(opt *Options, logger *zap.Logger) map[beaconprotocol.RoleType]controller.IController {
	ibfts := make(map[beaconprotocol.RoleType]controller.IController)
	ibfts[beaconprotocol.RoleTypeAttester] = setupIbftController(beaconprotocol.RoleTypeAttester, logger, opt.IbftStorage, opt.P2pNetwork, opt.Share, opt.ForkVersion, opt.Signer, opt.SyncRateLimit)
	return ibfts
}

func setupIbftController(
	role beaconprotocol.RoleType,
	logger *zap.Logger,
	ibftStorage qbftstorage.QBFTStore,
	network p2pprotocol.Network,
	share *message.Share,
	forkVersion forksprotocol.ForkVersion,
	signer beaconprotocol.Signer,
	syncRateLimit time.Duration) controller.IController {
	identifier := []byte(format.IdentifierFormat(share.PublicKey.Serialize(), role.String()))

	return controller.New(
		role,
		identifier,
		logger,
		ibftStorage,
		network,
		qbft.DefaultConsensusParams(),
		share,
		forkVersion,
		signer,
		syncRateLimit)
}
