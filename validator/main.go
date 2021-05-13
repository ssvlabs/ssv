package validator

import (
	"context"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/slotqueue"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/validator/storage"
	"go.uber.org/zap"
	"time"
)

// Options to add in validator struct creation
type ControllerOptions struct {
	Context                    context.Context
	DB                         *basedb.IDb
	Logger                     *zap.Logger
	SignatureCollectionTimeout time.Duration `yaml:"SignatureCollectionTimeout" env:"DUTY_LIMIT" env-default:"5" env-description:"Timeout for signature collection after consensus"`
	Network                    network.Network
	SlotQueue                  slotqueue.Queue
	Beacon                     *beacon.Beacon
}

// Controller interface
type IController interface {
	setupValidators() map[string]*Validator
	StartValidators() map[string]*Validator
}

// Validator struct that manages all ibft wrappers
type Controller struct {
	context                    context.Context
	collection                 storage.ICollection
	logger                     *zap.Logger
	signatureCollectionTimeout time.Duration
	slotQueue                  slotqueue.Queue
	beacon                     beacon.Beacon
	// TODO remove after IBFT refactor
	network     network.Network
	ibftStorage collections.IbftStorage
}

func NewController(options ControllerOptions) IController {
	ibftStorage := collections.NewIbft(options.DB, options.Logger, "attestation")

	collection := storage.NewCollection(storage.CollectionOptions{
		DB:     options.DB,
		Logger: options.Logger,
	})
	controller := Controller{
		collection:                 collection,
		context:                    options.Context,
		logger:                     options.Logger,
		signatureCollectionTimeout: options.SignatureCollectionTimeout,
		slotQueue:                  options.SlotQueue,
		beacon:                     *options.Beacon,
		ibftStorage:                ibftStorage,
	}
	return &controller
}

// setupValidators for each validatorShare with proper ibft wrappers
func (c *Controller) setupValidators() map[string]*Validator {
	validatorsShare, err := c.collection.GetAllValidatorsShare()
	if err != nil {
		c.logger.Fatal("Failed to get validatorStorage share", zap.Error(err))
	}

	res := make(map[string]*Validator)
	for _, validatorShare := range validatorsShare {
		res[validatorShare.PublicKey.SerializeToHexStr()] = New(Options{
			Context:                    c.context,
			SignatureCollectionTimeout: c.signatureCollectionTimeout,
			SlotQueue:                  c.slotQueue,
			Logger:                     c.logger,
			Share:                      validatorShare,
			Network:                    c.network,
		}, &c.ibftStorage)
	}
	c.logger.Info("setup validators done successfully", zap.Int("count", len(res)))
	return res
}

// startValidators functions (queue streaming, msgQueue listen, etc)
func (c *Controller) StartValidators() map[string]*Validator {
	validators := c.setupValidators()
	for _, v := range validators {
		if err := v.Start(); err != nil {
			c.logger.Error("failed to start validator", zap.Error(err))
			continue
		}
	}
	return validators
}
