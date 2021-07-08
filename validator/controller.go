package validator

import (
	"context"
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/pubsub"
	"github.com/bloxapp/ssv/slotqueue"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	validatorstorage "github.com/bloxapp/ssv/validator/storage"
	"go.uber.org/zap"
	"time"
)

// ControllerOptions for controller struct creation
type ControllerOptions struct {
	Context                    context.Context
	DB                         basedb.IDb
	Logger                     *zap.Logger
	SignatureCollectionTimeout time.Duration `yaml:"SignatureCollectionTimeout" env:"SIGNATURE_COLLECTION_TIMEOUT" env-default:"5s" env-description:"Timeout for signature collection after consensus"`
	ETHNetwork                 *core.Network
	Network                    network.Network
	SlotQueue                  slotqueue.Queue
	Beacon                     beacon.Beacon
	Shares                     []validatorstorage.ShareOptions `yaml:"Shares"`
	ShareEncryptionKeyProvider eth1.ShareEncryptionKeyProvider
}

// IController interface
type IController interface {
	ListenToEth1Events(cn pubsub.SubjectChannel)
	StartValidators() map[string]*Validator
	GetValidatorsPubKeys() [][]byte
	GetValidator(pubKey string) (*Validator, bool)
	NewValidatorSubject() pubsub.Subscriber
}

// Controller struct that manages all validator shares
type controller struct {
	context                    context.Context
	collection                 validatorstorage.ICollection
	logger                     *zap.Logger
	signatureCollectionTimeout time.Duration
	slotQueue                  slotqueue.Queue
	beacon                     beacon.Beacon
	// TODO remove after IBFT refactor
	network    network.Network
	db         basedb.IDb
	ethNetwork *core.Network

	validatorsMap       map[string]*Validator
	newValidatorSubject pubsub.Subject

	shareEncryptionKeyProvider eth1.ShareEncryptionKeyProvider
}

// NewController creates new validator controller
func NewController(options ControllerOptions) IController {
	collection := validatorstorage.NewCollection(validatorstorage.CollectionOptions{
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
		beacon:                     options.Beacon,
		db:                         options.DB,
		network:                    options.Network,
		ethNetwork:                 options.ETHNetwork,
		newValidatorSubject: pubsub.NewSubject(options.Logger.With(
			zap.String("which", "validator/controller/validator-subject"))),
		validatorsMap:              make(map[string]*Validator),
		shareEncryptionKeyProvider: options.ShareEncryptionKeyProvider,
	}

	return &ctrl
}

// ListenToEth1Events is listening to events coming from eth1 client
func (c *controller) ListenToEth1Events(cn pubsub.SubjectChannel) {
	for e := range cn {
		if event, ok := e.(eth1.Event); ok {
			if validatorAddedEvent, ok := event.Data.(eth1.ValidatorAddedEvent); ok {
				c.handleValidatorAddedEvent(validatorAddedEvent)
			}
		}
	}
}

// setupValidators for each validatorShare with proper ibft wrappers
func (c *controller) setupValidators() map[string]*Validator {
	validatorsShare, err := c.collection.GetAllValidatorsShare()
	if err != nil {
		c.logger.Fatal("Failed to get validators shares", zap.Error(err))
	}

	res := make(map[string]*Validator)
	for _, validatorShare := range validatorsShare {
		printValidatorShare(c.logger, validatorShare)
		res[validatorShare.PublicKey.SerializeToHexStr()] = New(Options{
			Context:                    c.context,
			SignatureCollectionTimeout: c.signatureCollectionTimeout,
			SlotQueue:                  c.slotQueue,
			Logger:                     c.logger,
			Share:                      validatorShare,
			Network:                    c.network,
			ETHNetwork:                 c.ethNetwork,
			Beacon:                     c.beacon,
		}, c.db)
	}
	c.validatorsMap = res
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

// GetValidatorsPubKeys returns a list of all the validators public keys
func (c *controller) GetValidatorsPubKeys() [][]byte {
	var pubKeys [][]byte
	for _, val := range c.validatorsMap {
		pubKeys = append(pubKeys, val.Share.PublicKey.Serialize())
	}
	return pubKeys
}

// GetValidator returns a validator
func (c *controller) GetValidator(pubKey string) (*Validator, bool) {
	v, ok := c.validatorsMap[pubKey]
	return v, ok
}

// AddValidator adds a new validator
func (c *controller) AddValidator(pubKey string, v *Validator) bool {
	if _, ok := c.validatorsMap[pubKey]; !ok {
		c.validatorsMap[pubKey] = v
		return true
	}
	c.logger.Info("validator already exist")
	return false
}

// NewValidatorSubject returns the validators subject
func (c *controller) NewValidatorSubject() pubsub.Subscriber {
	return c.newValidatorSubject
}

func (c *controller) handleValidatorAddedEvent(validatorAddedEvent eth1.ValidatorAddedEvent) {
	l := c.logger.With(zap.String("validatorPubKey", hex.EncodeToString(validatorAddedEvent.PublicKey)))
	l.Debug("handles validator added event")
	operatorPrivKey, err := c.shareEncryptionKeyProvider()
	if err != nil {
		l.Error("failed to get operator private key")
		return
	}
	var operatorPubKey string
	if operatorPrivKey != nil {
		operatorPubKey, err = rsaencryption.ExtractPublicKey(operatorPrivKey)
		if err != nil {
			l.Error("failed to extract operator public key")
			return
		}
	}
	validatorShare, err := ShareFromValidatorAddedEvent(validatorAddedEvent, operatorPubKey)
	if err != nil {
		l.Error("failed to create share", zap.Error(err))
		return
	}
	if len(validatorShare.Committee) > 0 {
		if err := c.collection.SaveValidatorShare(validatorShare); err != nil {
			l.Error("failed to save validator share", zap.Error(err))
			return
		}
		l.Debug("validator share was saved")
		c.onNewValidatorShare(validatorShare)
	}
}

func (c *controller) onNewValidatorShare(validatorShare *validatorstorage.Share) {
	pubKeyHex := validatorShare.PublicKey.SerializeToHexStr()
	if _, exist := c.GetValidator(pubKeyHex); exist {
		c.logger.Debug("skip setup for known validator",
			zap.String("pubKeyHex", pubKeyHex))
		return
	}
	// setup validator
	validatorOpts := Options{
		Context:                    c.context,
		Logger:                     c.logger,
		Share:                      validatorShare,
		Network:                    c.network,
		Beacon:                     c.beacon,
		ETHNetwork:                 c.ethNetwork,
		SlotQueue:                  c.slotQueue,
		SignatureCollectionTimeout: c.signatureCollectionTimeout,
	}
	v := New(validatorOpts, c.db)
	if added := c.AddValidator(pubKeyHex, v); added {
		// start validator
		if err := v.Start(); err != nil {
			c.logger.Error("failed to start validator",
				zap.Error(err), zap.String("pubKeyHex", pubKeyHex))
		} else {
			c.logger.Debug("validator started", zap.String("pubKeyHex", pubKeyHex))
		}
		c.newValidatorSubject.Notify(*v)
	}
}

func printValidatorShare(logger *zap.Logger, validatorShare *validatorstorage.Share) {
	var committee []string
	for _, c := range validatorShare.Committee {
		committee = append(committee, fmt.Sprintf(`[IbftId=%d, PK=%x]`, c.IbftId, c.Pk))
	}
	logger.Debug("setup validator",
		zap.String("pubKey", validatorShare.PublicKey.SerializeToHexStr()),
		zap.Uint64("nodeID", validatorShare.NodeID),
		zap.Strings("committee", committee))
}
