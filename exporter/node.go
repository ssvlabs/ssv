package exporter

import (
	"context"
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/exporter/ibft"
	"github.com/bloxapp/ssv/ibft/proto"
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
	"sync"
	"time"
)

const (
	ibftSyncDispatcherTick = 1 * time.Second
)

var (
	ibftSyncEnabled = false
)

// Exporter represents the main interface of this package
type Exporter interface {
	Start() error
	StartEth1() error
}

// Options contains options to create the node
type Options struct {
	Ctx context.Context

	Logger     *zap.Logger
	ETHNetwork *core.Network

	Eth1Client eth1.Client

	Network network.Network

	DB basedb.IDb
}

// exporter is the internal implementation of Exporter interface
type exporter struct {
	ctx              context.Context
	storage          Storage
	validatorStorage validatorstorage.ICollection
	ibftStorage      collections.Iibft
	logger           *zap.Logger
	network          network.Network
	eth1Client       eth1.Client
	ibftDisptcher    tasks.Dispatcher
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
	e := exporter{
		ctx:              opts.Ctx,
		storage:          NewExporterStorage(opts.DB, opts.Logger),
		ibftStorage:      &ibftStorage,
		validatorStorage: validatorStorage,
		logger:           opts.Logger,
		network:          opts.Network,
		eth1Client:       opts.Eth1Client,
		ibftDisptcher: tasks.NewDispatcher(tasks.DispatcherOptions{
			Ctx:      opts.Ctx,
			Logger:   opts.Logger,
			Interval: ibftSyncDispatcherTick,
		}),
	}

	return &e
}

// Start starts the IBFT dispatcher for syncing data nd listen to messages
func (exp *exporter) Start() error {
	exp.logger.Info("starting node -> IBFT")

	go exp.ibftDisptcher.Start()
	return nil
}

// StartEth1 starts the eth1 events sync and streaming
func (exp *exporter) StartEth1() error {
	exp.logger.Info("starting node -> eth1")

	// register for contract events that will arrive from eth1Client
	eth1EventChan, err := exp.eth1Client.EventsSubject().Register("Eth1ExporterObserver")
	if err != nil {
		return errors.Wrap(err, "could not register for eth1 events subject")
	}
	errCn := exp.listenToEth1Events(eth1EventChan)
	go func() {
		for err := range errCn {
			exp.logger.Warn("could not handle eth1 event", zap.Error(err))
		}
	}()
	// sync events
	var syncErr error
	var syncProcess sync.WaitGroup
	syncProcess.Add(1)
	go func() {
		defer syncProcess.Done()
		syncErr = eth1.SyncEth1Events(exp.logger, exp.eth1Client, exp.storage, "ExporterSync")
	}()
	syncProcess.Wait()
	if syncErr != nil {
		return errors.Wrap(syncErr, "failed to sync eth1 contract events")
	}
	exp.logger.Debug("sync was done successfully")
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
	exp.logger.Info("validator added event", zap.String("pubKey", pubKeyHex))
	validatorShare, err := validator.ShareFromValidatorAddedEvent(event, true)
	if err != nil {
		return errors.Wrap(err, "could not create a share from ValidatorAddedEvent")
	}
	if err := exp.validatorStorage.SaveValidatorShare(validatorShare); err != nil {
		return errors.Wrap(err, "failed to save validator share")
	}

	exp.logger.Debug("validator share was saved", zap.String("pubKey", pubKeyHex))
	// triggers a sync for the given validator
	if err = exp.triggerIBFTSync(validatorShare.PublicKey); err != nil {
		return errors.Wrap(err, "failed to trigger ibft sync")
	}

	return nil
}

func (exp *exporter) handleOperatorAddedEvent(event eth1.OperatorAddedEvent) error {
	exp.logger.Info("operator added event",
		zap.String("pubKey", hex.EncodeToString(event.PublicKey)))

	oi := OperatorInformation{
		PublicKey:    event.PublicKey,
		Name:         event.Name,
		OwnerAddress: event.OwnerAddress,
	}
	err := exp.storage.SaveOperatorInformation(&oi)
	if err != nil {
		return err
	}
	exp.logger.Debug("managed to save operator information",
		zap.String("pubKey", hex.EncodeToString(event.PublicKey)))
	return nil
}

func (exp *exporter) triggerIBFTSync(validatorPubKey *bls.PublicKey) error {
	if !ibftSyncEnabled {
		return nil
	}
	validatorShare, err := exp.validatorStorage.GetValidatorsShare(validatorPubKey.Serialize())
	if err != nil {
		return errors.Wrap(err, "could not get validator share")
	}
	exp.logger.Debug("syncing ibft data for validator",
		zap.String("pubKey", validatorPubKey.SerializeToHexStr()))
	ibftInstance := ibft.NewIbftReadOnly(ibft.ReaderOptions{
		Logger:         exp.logger,
		Storage:        exp.ibftStorage,
		Network:        exp.network,
		Config:         proto.DefaultConsensusParams(),
		ValidatorShare: validatorShare,
	})

	t := newIbftSyncTask(ibftInstance, validatorPubKey.SerializeToHexStr())
	exp.ibftDisptcher.Queue(t)

	return nil
}

func newIbftSyncTask(ibftReader ibft.Reader, pubKeyHex string) tasks.Task {
	tid := fmt.Sprintf("ibft:sync/%s", pubKeyHex)
	return *tasks.NewTask(ibftReader.Sync, tid)
}
