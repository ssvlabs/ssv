package exporter

import (
	"context"
	"encoding/hex"
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
	"github.com/bloxapp/ssv/validator"
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
	ibftSyncEnabled = false
	syncWhitelist   []string
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

	WS        api.WebSocketServer
	WsAPIPort int
}

// exporter is the internal implementation of Exporter interface
type exporter struct {
	ctx              context.Context
	storage          storage.Storage
	validatorStorage validatorstorage.ICollection
	ibftStorage      collections.Iibft
	logger           *zap.Logger
	network          network.Network
	eth1Client       eth1.Client
	ibftDisptcher    tasks.Dispatcher
	ws               api.WebSocketServer
	wsAPIPort        int
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
		ibftDisptcher: tasks.NewDispatcher(tasks.DispatcherOptions{
			Ctx:      opts.Ctx,
			Logger:   opts.Logger.With(zap.String("component", "ibftDispatcher")),
			Interval: ibftSyncDispatcherTick,
		}),
		ws:        opts.WS,
		wsAPIPort: opts.WsAPIPort,
	}

	return &e
}

// Start starts the IBFT dispatcher for syncing data nd listen to messages
func (exp *exporter) Start() error {
	exp.logger.Info("starting node")

	go exp.ibftDisptcher.Start()

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

// ListenToEth1Events register for eth1 events
func (exp *exporter) listenToEth1Events(cn pubsub.SubjectChannel) chan error {
	cnErr := make(chan error)
	go func() {
		for e := range cn {
			if event, ok := e.(eth1.Event); ok {
				var err error = nil
				if validatorAddedEvent, ok := event.Data.(eth1.ValidatorAddedEvent); ok {
					err = exp.handleValidatorAddedEvent(validatorAddedEvent)
				} else if opertaorAddedEvent, ok := event.Data.(eth1.OperatorAddedEvent); ok {
					err = exp.handleOperatorAddedEvent(opertaorAddedEvent)
				}
				if err != nil {
					cnErr <- err
				}
			}
		}
	}()
	return cnErr
}

// handleValidatorAddedEvent parses the given event and sync the ibft-data of the validator
func (exp *exporter) handleValidatorAddedEvent(event eth1.ValidatorAddedEvent) error {
	pubKeyHex := hex.EncodeToString(event.PublicKey)
	logger := exp.logger.With(zap.String("pubKey", pubKeyHex))
	logger.Info("validator added event")
	// save the share to be able to reuse IBFT functionality
	validatorShare, err := validator.ShareFromValidatorAddedEvent(event, "")
	if err != nil {
		return errors.Wrap(err, "could not create a share from ValidatorAddedEvent")
	}
	if err := exp.validatorStorage.SaveValidatorShare(validatorShare); err != nil {
		return errors.Wrap(err, "failed to save validator share")
	}
	logger.Debug("validator share was saved")
	// save information for exporting validators
	vi, err := toValidatorInformation(event)
	if err != nil {
		return errors.Wrap(err, "could not create ValidatorInformation")
	}
	if err := exp.storage.SaveValidatorInformation(vi); err != nil {
		return errors.Wrap(err, "failed to save validator information")
	}
	logger.Debug("validator information was saved")

	// TODO: aggregate validators in sync scenario
	// otherwise the network will be overloaded with multiple messages
	exp.ws.OutboundSubject().Notify(api.NetworkMessage{Msg: api.Message{
		Type:   api.TypeValidator,
		Filter: api.MessageFilter{From: vi.Index, To: vi.Index},
		Data:   []storage.ValidatorInformation{*vi},
	}, Conn: nil})

	// triggers a sync for the given validator
	if err = exp.triggerIBFTSync(validatorShare.PublicKey); err != nil {
		return errors.Wrap(err, "failed to trigger ibft sync")
	}

	return nil
}

func (exp *exporter) handleOperatorAddedEvent(event eth1.OperatorAddedEvent) error {
	l := exp.logger.With(zap.String("pubKey", string(event.PublicKey)))
	l.Info("operator added event")
	oi := storage.OperatorInformation{
		PublicKey:    string(event.PublicKey),
		Name:         event.Name,
		OwnerAddress: event.OwnerAddress,
	}
	err := exp.storage.SaveOperatorInformation(&oi)
	if err != nil {
		return err
	}
	l.Debug("managed to save operator information")

	exp.ws.OutboundSubject().Notify(api.NetworkMessage{Msg: api.Message{
		Type:   api.TypeOperator,
		Filter: api.MessageFilter{From: oi.Index, To: oi.Index},
		Data:   []storage.OperatorInformation{oi},
	}, Conn: nil})

	return nil
}

// toValidatorInformation converts raw event to ValidatorInformation
func toValidatorInformation(validatorAddedEvent eth1.ValidatorAddedEvent) (*storage.ValidatorInformation, error) {
	pubKey := &bls.PublicKey{}
	if err := pubKey.Deserialize(validatorAddedEvent.PublicKey); err != nil {
		return nil, errors.Wrap(err, "failed to deserialize validator public key")
	}

	var operators []storage.OperatorNodeLink
	for i := range validatorAddedEvent.OessList {
		oess := validatorAddedEvent.OessList[i]
		nodeID := oess.Index.Uint64() + 1
		operators = append(operators, storage.OperatorNodeLink{
			ID: nodeID, PublicKey: string(oess.OperatorPublicKey),
		})
	}

	vi := storage.ValidatorInformation{
		PublicKey: pubKey.SerializeToHexStr(),
		Operators: operators,
	}

	return &vi, nil
}

func (exp *exporter) shouldSyncIbft(pubkey string) bool {
	for _, pk := range syncWhitelist {
		if pubkey == pk {
			return true
		}
	}
	return ibftSyncEnabled
}

func (exp *exporter) triggerIBFTSync(validatorPubKey *bls.PublicKey) error {
	if validatorPubKey == nil {
		return errors.New("empty validator pubkey")
	}
	pubkey := validatorPubKey.SerializeToHexStr()
	if !exp.shouldSyncIbft(pubkey) {
		return nil
	}
	validatorShare, found, err := exp.validatorStorage.GetValidatorsShare(validatorPubKey.Serialize())
	if !found{
		return errors.New("could not find validator share")
	}
	if err != nil {
		return errors.Wrap(err, "could not get validator share")
	}
	exp.logger.Debug("ibft sync was triggered",
		zap.String("pubKey", pubkey))
	ibftDecidedReader := ibft.NewIbftDecidedReadOnly(ibft.DecidedReaderOptions{
		Logger:         exp.logger,
		Storage:        exp.ibftStorage,
		Network:        exp.network,
		Config:         proto.DefaultConsensusParams(),
		ValidatorShare: validatorShare,
	})
	t := newIbftReaderTask(ibftDecidedReader, "sync", pubkey)
	exp.ibftDisptcher.Queue(t)

	ibftMsgReader := ibft.NewIncomingMsgsReader(ibft.IncomingMsgsReaderOptions{
		Logger:  exp.logger,
		Network: exp.network,
		Config:  proto.DefaultConsensusParams(),
		PK:      validatorPubKey,
	})
	t2 := newIbftReaderTask(ibftMsgReader, "msgReader", validatorPubKey.SerializeToHexStr())
	exp.ibftDisptcher.Queue(t2)

	return nil
}

func newIbftReaderTask(ibftReader ibft.Reader, prefix, pubKeyHex string) tasks.Task {
	return *tasks.NewTask(ibftReader.Start, fmt.Sprintf("ibft:%s/%s", prefix, pubKeyHex))
}
