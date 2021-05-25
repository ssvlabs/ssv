package validator

import (
	"context"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/slotqueue"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/bloxapp/ssv/validator/storage"
	"go.uber.org/zap"
	"time"
)

// ControllerOptions for controller struct creation
type ControllerOptions struct {
	Context                    context.Context
	DB                         *basedb.IDb
	Logger                     *zap.Logger
	SignatureCollectionTimeout time.Duration `yaml:"SignatureCollectionTimeout" env:"SIGNATURE_COLLECTION_TIMEOUT" env-default:"5s" env-description:"Timeout for signature collection after consensus"`
	ETHNetwork                 *core.Network
	Network                    network.Network
	SlotQueue                  slotqueue.Queue
	Beacon                     *beacon.Beacon
	Shares                     []storage.ShareOptions `yaml:"Shares"`
}

// IController interface
type IController interface {
	setupValidators() map[string]*Validator
	StartValidators() map[string]*Validator
}

// Controller struct that manages all validator shares
type controller struct {
	context                    context.Context
	collection                 storage.ICollection
	logger                     *zap.Logger
	signatureCollectionTimeout time.Duration
	slotQueue                  slotqueue.Queue
	beacon                     beacon.Beacon
	// TODO remove after IBFT refactor
	network     network.Network
	ibftStorage collections.IbftStorage
	ethNetwork  *core.Network
}

// NewController creates new validator controller
func NewController(options ControllerOptions) IController {
	ibftStorage := collections.NewIbft(options.DB, options.Logger, "attestation")

	collection := storage.NewCollection(storage.CollectionOptions{
		DB:     options.DB,
		Logger: options.Logger,
	})

	err := collection.LoadMultipleFromConfig(options.Shares)
	if err != nil {
		options.Logger.Error("failed to load all validators from config", zap.Error(err))
	}

	ctrl := controller{
		collection:                 collection,
		context:                    options.Context,
		logger:                     options.Logger,
		signatureCollectionTimeout: options.SignatureCollectionTimeout,
		slotQueue:                  options.SlotQueue,
		beacon:                     *options.Beacon,
		ibftStorage:                ibftStorage,
		network:                    options.Network,
		ethNetwork:                 options.ETHNetwork,
	}

	return &ctrl
}

// setupValidators for each validatorShare with proper ibft wrappers
func (c *controller) setupValidators() map[string]*Validator {
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
			ETHNetwork:                 c.ethNetwork,
			Beacon:                     c.beacon,
		}, &c.ibftStorage)
	}
	c.logger.Info("setup validators done successfully", zap.Int("count", len(res)))
	return res
}

// StartValidators functions (queue streaming, msgQueue listen, etc)
func (c *controller) StartValidators() map[string]*Validator {
	validators := c.setupValidators()
	for _, v := range validators {
		if err := v.Start(); err != nil {
			c.logger.Error("failed to start validator", zap.Error(err))
			continue
		}
	}
	return validators
}
