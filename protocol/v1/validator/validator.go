package validator

import (
	"context"
	"io"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/controller"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
)

// IValidator is the interface for validator
type IValidator interface {
	Start() error
	ExecuteDuty(slot uint64, duty *beaconprotocol.Duty)
	ProcessMsg(msg *message.SSVMessage) error // TODO need to be as separate interface?
	GetShare() *beaconprotocol.Share

	forksprotocol.ForkHandler
	io.Closer
}

// Options is the validator options
type Options struct {
	Context                    context.Context
	Logger                     *zap.Logger
	IbftStorage                qbftstorage.QBFTStore
	Network                    beaconprotocol.Network
	P2pNetwork                 p2pprotocol.Network
	Beacon                     beaconprotocol.Beacon
	Share                      *beaconprotocol.Share
	ForkVersion                forksprotocol.ForkVersion
	Signer                     beaconprotocol.Signer
	SyncRateLimit              time.Duration
	SignatureCollectionTimeout time.Duration
	ReadMode                   bool
	FullNode                   bool
	NewDecidedHandler          controller.NewDecidedHandler
}

// Validator represents the validator
type Validator struct {
	ctx        context.Context
	cancelCtx  context.CancelFunc
	logger     *zap.Logger
	network    beaconprotocol.Network
	p2pNetwork p2pprotocol.Network
	beacon     beaconprotocol.Beacon
	Share      *beaconprotocol.Share // var is exported to validator ctrl tests reasons
	signer     beaconprotocol.Signer

	ibfts controller.Controllers

	// flags
	readMode    bool
	saveHistory bool
}

// Ibfts returns the ibft controllers
func (v *Validator) Ibfts() controller.Controllers {
	return v.ibfts
}

// NewValidator creates a new validator
func NewValidator(opt *Options) IValidator {
	logger := opt.Logger.With(zap.String("pubKey", opt.Share.PublicKey.SerializeToHexStr())).
		With(zap.Uint64("node_id", uint64(opt.Share.NodeID)))

	//ctx, cancel := context.WithCancel(opt.Context)
	//optsCp := *opt
	//optsCp.Context = ctx
	//ibfts := setupIbfts(&optsCp, logger)
	ibfts := setupIbfts(opt, logger)

	if !opt.ReadMode {
		logger.Debug("new validator instance was created", zap.Strings("operators ids", opt.Share.HashOperators()))
	}

	ctx, cancel := context.WithCancel(opt.Context)
	return &Validator{
		ctx:         ctx,
		cancelCtx:   cancel,
		logger:      logger,
		network:     opt.Network,
		p2pNetwork:  opt.P2pNetwork,
		beacon:      opt.Beacon,
		Share:       opt.Share,
		signer:      opt.Signer,
		ibfts:       ibfts,
		readMode:    opt.ReadMode,
		saveHistory: opt.FullNode,
	}
}

// Close implements io.Closer
func (v *Validator) Close() error {
	v.cancelCtx()
	return nil
}

// Start starts the validator
func (v *Validator) Start() error {
	if err := v.p2pNetwork.Subscribe(v.GetShare().PublicKey.Serialize()); err != nil {
		return errors.Wrap(err, "failed to subscribe topic")
	}

	// init all ibft controllers
	for _, ib := range v.ibfts {
		go func(ib controller.IController) {
			if err := ib.Init(); err != nil {
				if err == controller.ErrAlreadyRunning {
					v.logger.Debug("ibft init is already running")
					return
				}
				v.logger.Error("could not initialize ibft instance", zap.Error(err))
			}
		}(ib)
	}

	return nil
}

// GetShare returns the validator share
func (v *Validator) GetShare() *beaconprotocol.Share {
	// TODO need lock?
	return v.Share
}

// ProcessMsg processes a new msg
func (v *Validator) ProcessMsg(msg *message.SSVMessage) error {
	ibftController := v.ibfts.ControllerForIdentifier(msg.GetIdentifier())
	// synchronize process
	return ibftController.ProcessMsg(msg)
}

// OnFork updates all QFBT controllers with the new fork version
func (v *Validator) OnFork(forkVersion forksprotocol.ForkVersion) error {
	for _, ctrl := range v.ibfts {
		if err := ctrl.OnFork(forkVersion); err != nil {
			return err
		}
	}
	return nil
}

// setupRunners return duty runners map with all the supported duty types
func setupIbfts(opt *Options, logger *zap.Logger) map[message.RoleType]controller.IController {
	ibfts := make(map[message.RoleType]controller.IController)
	ibfts[message.RoleTypeAttester] = setupIbftController(message.RoleTypeAttester, logger, opt)
	return ibfts
}

func setupIbftController(role message.RoleType, logger *zap.Logger, opt *Options) controller.IController {
	identifier := message.NewIdentifier(opt.Share.PublicKey.Serialize(), role)
	opts := controller.Options{
		Context:           opt.Context,
		Role:              role,
		Identifier:        identifier,
		Logger:            logger,
		Storage:           opt.IbftStorage,
		Network:           opt.P2pNetwork,
		InstanceConfig:    qbft.DefaultConsensusParams(),
		ValidatorShare:    opt.Share,
		Version:           opt.ForkVersion,
		Beacon:            opt.Beacon,
		Signer:            opt.Signer,
		SyncRateLimit:     opt.SyncRateLimit,
		SigTimeout:        opt.SignatureCollectionTimeout,
		ReadMode:          opt.ReadMode,
		FullNode:          opt.FullNode,
		NewDecidedHandler: opt.NewDecidedHandler,
	}
	return controller.New(opts)
}
