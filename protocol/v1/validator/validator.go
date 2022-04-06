package validator

import (
	"context"
	"github.com/bloxapp/ssv/protocol/v1/queue/worker"
	"go.uber.org/zap"
	"time"

	ibftctrl "github.com/bloxapp/ssv/ibft/controller"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/msgqueue"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	keymanagerprotocol "github.com/bloxapp/ssv/protocol/v1/keymanager"
	"github.com/bloxapp/ssv/protocol/v1/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/bloxapp/ssv/protocol/v1/qbft/controller"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/utils/format"
)

type IValidator interface {
	Start()
	ExecuteDuty(slot uint64, duty *beaconprotocol.Duty)
	ProcessMsg(msg *message.SSVMessage) //TODO need to be as separate interface?
	GetShare() *keymanagerprotocol.Share
}

type Options struct {
	Context  context.Context
	Logger   *zap.Logger
	Network  p2pprotocol.Network
	Beacon   beaconprotocol.Beacon
	Share    *keymanagerprotocol.Share
	ReadMode bool
}

type Validator struct {
	ctx     context.Context
	logger  *zap.Logger
	network p2pprotocol.Network
	beacon  beaconprotocol.Beacon
	share   *keymanagerprotocol.Share
	worker  *worker.Worker

	ibfts map[beaconprotocol.RoleType]controller.IController

	readMode bool
}

func NewValidator(opt *Options) IValidator {
	logger := opt.Logger.With(zap.String("pubKey", opt.Share.PublicKey.SerializeToHexStr())).
		With(zap.Uint64("node_id", uint64(opt.Share.NodeID)))

	workerCfg := &worker.WorkerConfig{
		Ctx:          opt.Context,
		WorkersCount: 1,   // TODO flag
		Buffer:       100, // TODO flag
	}
	queueWorker := worker.NewWorker(workerCfg)

	ibfts := setupIbfts(opt, logger)

	logger.Debug("new validator instance was created", zap.Strings("operators ids", opt.Share.HashOperators()))
	return &Validator{
		ctx:      opt.Context,
		logger:   logger,
		network:  opt.Network,
		beacon:   opt.Beacon,
		share:    opt.Share,
		ibfts:    ibfts,
		worker:   queueWorker,
		readMode: opt.ReadMode,
	}
}

func (v *Validator) Start() {
	// start queue workers
	v.worker.AddHandler(v.messageHandler)
}

func (v *Validator) ExecuteDuty(slot uint64, duty *beaconprotocol.Duty) {
	// TODO implement me
	panic("implement me")
}

func (v *Validator) GetShare() *keymanagerprotocol.Share {
	// TODO need lock?
	return v.share
}

// setupRunners return duty runners map with all the supported duty types
func setupIbfts(opt *Options, logger *zap.Logger) map[beaconprotocol.RoleType]controller.IController {
	ibfts := make(map[beaconprotocol.RoleType]controller.IController)
	ibfts[beaconprotocol.RoleTypeAttester] = setupIbftController(beaconprotocol.RoleTypeAttester, logger, opt.DB, opt.Network, msgQueue, opt.Share, opt.ForkVersion, opt.Signer, opt.SyncRateLimit)
	return ibfts
}

func setupIbftController(role beaconprotocol.RoleType, logger *zap.Logger, db basedb.IDb, network network.P2PNetwork, msgQueue *msgqueue.MessageQueue, share *keymanagerprotocol.Share, forkVersion forksprotocol.ForkVersion, signer beaconprotocol.Signer, syncRateLimit time.Duration) controller.IController {
	ibftStorage := collections.NewIbft(db, logger, role.String())
	identifier := []byte(format.IdentifierFormat(share.PublicKey.Serialize(), role.String()))

	return ibftctrl.New(
		role,
		identifier,
		logger,
		&ibftStorage,
		network,
		msgQueue,
		proto.DefaultConsensusParams(),
		share,
		forkVersion,
		signer,
		syncRateLimit)
}
