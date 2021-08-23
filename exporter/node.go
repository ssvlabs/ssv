package exporter

import (
	"context"
	"fmt"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/exporter/api"
	"github.com/bloxapp/ssv/exporter/ibft"
	"github.com/bloxapp/ssv/exporter/storage"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/metrics"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/pubsub"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/utils/tasks"
	validatorstorage "github.com/bloxapp/ssv/validator/storage"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"time"
)

const (
	ibftSyncDispatcherTick = 1 * time.Second
)

var (
	syncWhitelist []string
)

// Exporter represents the main interface of this package
type Exporter interface {
	Start() error
	StartEth1(syncOffset *eth1.SyncOffset) error
}

// Options contains options to create the node
type Options struct {
	Ctx context.Context

	Logger     *zap.Logger
	ETHNetwork *core.Network

	Eth1Client eth1.Client

	Network network.Network

	DB basedb.IDb

	WS              api.WebSocketServer
	WsAPIPort       int
	IbftSyncEnabled bool
}

// exporter is the internal implementation of Exporter interface
type exporter struct {
	ctx                   context.Context
	storage               storage.Storage
	validatorStorage      validatorstorage.ICollection
	ibftStorage           collections.Iibft
	logger                *zap.Logger
	network               network.Network
	eth1Client            eth1.Client
	ibftSyncDispatcher    tasks.Dispatcher
	networkReadDispatcher tasks.Dispatcher
	ws                    api.WebSocketServer
	wsAPIPort             int
	ibftSyncEnabled       bool
}

// New creates a new Exporter instance
func New(opts Options) Exporter {
	validatorStorage := validatorstorage.NewCollection(
		validatorstorage.CollectionOptions{
			DB:     opts.DB,
			Logger: opts.Logger,
		},
	)
	ibftStorage := collections.NewIbft(opts.DB, opts.Logger, "attestation")
	logger := opts.Logger.With(zap.String("component", "exporter/node"))
	e := exporter{
		ctx:              opts.Ctx,
		storage:          storage.NewExporterStorage(opts.DB, opts.Logger),
		ibftStorage:      &ibftStorage,
		validatorStorage: validatorStorage,
		logger:           logger,
		network:          opts.Network,
		eth1Client:       opts.Eth1Client,
		ibftSyncDispatcher: tasks.NewDispatcher(tasks.DispatcherOptions{
			Ctx:      opts.Ctx,
			Logger:   opts.Logger.With(zap.String("component", "ibftSyncDispatcher")),
			Interval: ibftSyncDispatcherTick,
		}),
		networkReadDispatcher: tasks.NewDispatcher(tasks.DispatcherOptions{
			Ctx:        opts.Ctx,
			Logger:     opts.Logger.With(zap.String("component", "networkReadDispatcher")),
			Interval:   ibftSyncDispatcherTick,
			Concurrent: 1000, // using a large limit of concurrency as listening to network messages remains open
		}),
		ws:              opts.WS,
		wsAPIPort:       opts.WsAPIPort,
		ibftSyncEnabled: opts.IbftSyncEnabled,
	}

	return &e
}

// Start starts the IBFT dispatcher for syncing data nd listen to messages
func (exp *exporter) Start() error {
	exp.logger.Info("starting node")

	go exp.ibftSyncDispatcher.Start()
	go exp.networkReadDispatcher.Start()

	if exp.ws == nil {
		return nil
	}

	go func() {
		cn, err := exp.ws.IncomingSubject().Register("exporter-node")
		if err != nil {
			exp.logger.Error("could not register for incoming messages", zap.Error(err))
		}
		defer exp.ws.IncomingSubject().Deregister("exporter-node")

		exp.processIncomingExportRequests(cn, exp.ws.OutboundSubject())
	}()

	go exp.triggerAllValidators()

	return exp.ws.Start(fmt.Sprintf(":%d", exp.wsAPIPort))
}

// HealthCheck returns a list of issues regards the state of the exporter node
func (exp *exporter) HealthCheck() []string {
	return metrics.ProcessAgents(exp.healthAgents())
}

func (exp *exporter) healthAgents() []metrics.HealthCheckAgent {
	var agents []metrics.HealthCheckAgent
	if agent, ok := exp.eth1Client.(metrics.HealthCheckAgent); ok {
		agents = append(agents, agent)
	}
	return agents
}

// processIncomingExportRequests waits for incoming messages and
func (exp *exporter) processIncomingExportRequests(incoming pubsub.SubjectChannel, outbound pubsub.Publisher) {
	for raw := range incoming {
		nm, ok := raw.(api.NetworkMessage)
		if !ok {
			exp.logger.Warn("could not parse export request message")
			nm = api.NetworkMessage{Msg: api.Message{Type: api.TypeError, Data: []string{"could not parse network message"}}}
		}
		if nm.Err != nil {
			nm.Msg = api.Message{Type: api.TypeError, Data: []string{"could not parse network message"}}
		}
		exp.logger.Debug("got incoming export request",
			zap.String("type", string(nm.Msg.Type)))
		switch nm.Msg.Type {
		case api.TypeOperator:
			handleOperatorsQuery(exp.logger, exp.storage, &nm)
		case api.TypeValidator:
			handleValidatorsQuery(exp.logger, exp.storage, &nm)
		case api.TypeIBFT:
			handleDutiesQuery(exp.logger, &nm)
		case api.TypeError:
			handleErrorQuery(exp.logger, &nm)
		default:
			handleUnknownQuery(exp.logger, &nm)
		}
		outbound.Notify(nm)
	}
}

// StartEth1 starts the eth1 events sync and streaming
func (exp *exporter) StartEth1(syncOffset *eth1.SyncOffset) error {
	exp.logger.Info("starting node -> eth1")

	// register for contract events that will arrive from eth1Client
	eth1EventChan, err := exp.eth1Client.EventsSubject().Register("Eth1ExporterObserver")
	if err != nil {
		return errors.Wrap(err, "could not register for eth1 events subject")
	}
	errCn := exp.listenToEth1Events(eth1EventChan)
	go func() {
		// log errors while processing events
		for err := range errCn {
			exp.logger.Warn("could not handle eth1 event", zap.Error(err))
		}
	}()
	// sync events
	syncErr := eth1.SyncEth1Events(exp.logger, exp.eth1Client, exp.storage, "ExporterSync", syncOffset)
	if syncErr != nil {
		return errors.Wrap(syncErr, "failed to sync eth1 contract events")
	}
	exp.logger.Info("manage to sync contract events")

	// start events stream
	err = exp.eth1Client.Start()
	if err != nil {
		return errors.Wrap(err, "could not start eth1 client")
	}
	return nil
}

func (exp *exporter) triggerAllValidators() {
	shares, err := exp.validatorStorage.GetAllValidatorsShare()
	if err != nil {
		exp.logger.Error("could not get validators shares", zap.Error(err))
		return
	}
	for _, share := range shares {
		if err = exp.triggerValidator(share.PublicKey); err != nil {
			exp.logger.Error("failed to trigger ibft sync", zap.Error(err),
				zap.String("pubKey", share.PublicKey.SerializeToHexStr()))
		}
	}
}

func (exp *exporter) shouldProcessValidator(pubkey string) bool {
	for _, pk := range syncWhitelist {
		if pubkey == pk {
			return true
		}
	}
	return exp.ibftSyncEnabled
}

func (exp *exporter) triggerValidator(validatorPubKey *bls.PublicKey) error {
	if validatorPubKey == nil {
		return errors.New("empty validator pubkey")
	}
	pubkey := validatorPubKey.SerializeToHexStr()
	if !exp.shouldProcessValidator(pubkey) {
		return nil
	}
	validatorShare, found, err := exp.validatorStorage.GetValidatorsShare(validatorPubKey.Serialize())
	if !found {
		return errors.New("could not find validator share")
	}
	if err != nil {
		return errors.Wrap(err, "could not get validator share")
	}
	logger := exp.logger.With(zap.String("pubKey", pubkey))
	logger.Debug("ibft sync was triggered")

	syncDecidedReader := ibft.NewIbftDecidedReadOnly(ibft.DecidedReaderOptions{
		Logger:         exp.logger,
		Storage:        exp.ibftStorage,
		Network:        exp.network,
		Config:         proto.DefaultConsensusParams(),
		ValidatorShare: validatorShare,
	})

	syncTask := tasks.NewTask(syncDecidedReader.Start, fmt.Sprintf("ibft:sync/%s", pubkey), func() {
		logger.Debug("sync is done, starting to read network messages")
		exp.readNetworkMessages(validatorPubKey)
	})
	exp.ibftSyncDispatcher.Queue(*syncTask)

	return nil
}

func (exp *exporter) readNetworkMessages(validatorPubKey *bls.PublicKey) {
	ibftMsgReader := ibft.NewIncomingMsgsReader(ibft.IncomingMsgsReaderOptions{
		Logger:  exp.logger,
		Network: exp.network,
		Config:  proto.DefaultConsensusParams(),
		PK:      validatorPubKey,
	})
	readerTask := tasks.NewTask(ibftMsgReader.Start,
		fmt.Sprintf("ibft:msgReader/%s", validatorPubKey.SerializeToHexStr()), nil)
	exp.networkReadDispatcher.Queue(*readerTask)
}
