package exporter

import (
	"fmt"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/exporter/storage"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/storage/basedb"
	"go.uber.org/zap"
	"math/big"
)

const (
	defaultOffset string = "49e08f"
)

// Exporter represents the main interface of this package
type Exporter interface {
	Start() error
	Sync() error
}

// Options contains options to create the node
type Options struct {
	APIPort int `yaml:"APIPort" env-default:"5001"`

	Logger     *zap.Logger
	ETHNetwork *core.Network

	Eth1Client eth1.Client

	Network network.Network

	DB basedb.IDb
}

// exporter is the internal implementation of Exporter interface
type exporter struct {
	store storage.ExporterStorage

	logger     *zap.Logger
	network    network.Network
	eth1Client eth1.Client

	httpHandlers apiHandlers
}

// New creates a new Exporter instance
func New(opts Options) Exporter {
	e := exporter{
		store:      storage.NewExporterStorage(opts.DB, opts.Logger),
		logger:     opts.Logger,
		network:    opts.Network,
		eth1Client: opts.Eth1Client,
	}
	e.httpHandlers = newHTTPHandlers(&e, opts.APIPort)
	go e.HandleEth1Events()
	return &e
}

// Start starts the exporter
func (exp *exporter) Start() error {
	exp.logger.Info("exporter.Start()")
	err := exp.eth1Client.Start()
	if err != nil {
		exp.logger.Error("could not start eth1 client")
	}
	return exp.httpHandlers.Listen()
}

// Sync takes care of syncing an exporter node with:
//  1. ibft data from ssv nodes
//  2. registry data (validator/operator added) from eth1 contract
func (exp *exporter) Sync() error {
	exp.logger.Info("exporter.Sync()")
	offset := exp.syncOffset()
	return exp.eth1Client.Sync(offset)
}

func (exp *exporter) HandleEth1Events() {
	go func() {
		cn, err := exp.eth1Client.Subject().Register("ExporterObserver")
		if err != nil {
			exp.logger.Error("could not register for eth1 events channel", zap.Error(err))
			return
		}
		offsetRaw := exp.syncOffset()
		offset := offsetRaw.Uint64()
		var errs []error
		for e := range cn {
			exp.logger.Debug("got new eth1 event in exporter")
			if event, ok := e.(eth1.Event); ok {
				var err error
				if validatorAddedEvent, ok := event.Data.(eth1.ValidatorAddedEvent); ok {
					err = exp.handleValidatorAddedEvent(validatorAddedEvent)
				} else if opertaorAddedEvent, ok := event.Data.(eth1.OperatorAddedEvent); ok {
					err = exp.handleOperatorAddedEvent(opertaorAddedEvent)
				} else if _, ok := event.Data.(eth1.SyncEndedEvent); ok {
					// upgrade the offset (in DB) once sync is over
					if offset > offsetRaw.Uint64() {
						exp.logger.Debug("upgrading sync offset", zap.Uint64("offset", offset))
						offsetRaw.SetUint64(offset)
						exp.store.SaveSyncOffset(offsetRaw)
					}
					continue
				}
				// If things went well - save new offset
				if err == nil {
					// avoid updating the local offset variable with new values
					// this will make sure that the offset is not being upgraded if there are some missing events
					// TODO: handle deadlock (some broken event prevents from exporter to upgrade offset)
					if len(errs) > 0 {
						continue
					}
					blockNumber := event.Log.BlockNumber
					if blockNumber > offset {
						offset = blockNumber
					}
				} else {
					errs = append(errs, err)
				}
			}
		}
		exp.logger.Info("done reading messages from channel")
		// TODO handle errs?
	}()
}

func (exp *exporter) syncOffset() *big.Int {
	offset, err := exp.store.GetSyncOffset()
	if err != nil {
		offset = new(big.Int)
		exp.logger.Debug("could not get sync offset, using default offset")
		offset.SetString(defaultOffset, 16)
	}
	return offset
}

func (exp *exporter) handleValidatorAddedEvent(event eth1.ValidatorAddedEvent) error {
	exp.logger.Info(fmt.Sprintf("validator added: %x", event.PublicKey))
	// TODO: save validator data
	// TODO: use the pubkey to sync ibft data for the given validator
	return nil
}

func (exp *exporter) handleOperatorAddedEvent(event eth1.OperatorAddedEvent) error {
	exp.logger.Info(fmt.Sprintf("operator added: %x", event.PublicKey))
	// TODO: save operator data
	return nil
}

//func (exp *exporter) syncIbftData(validatorPubKey []byte) error {
//	exp.logger.Info(fmt.Sprintf("syncing ibft data for validator: %x", validatorPubKey))
//	// TODO: sync data for a given validator pubkey
//	return nil
//}
