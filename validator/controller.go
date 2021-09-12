package validator

import (
	"context"
	"encoding/hex"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/pubsub"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	validatorstorage "github.com/bloxapp/ssv/validator/storage"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync"
	"time"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
)

// ControllerOptions for controller struct creation
type ControllerOptions struct {
	Context                    context.Context
	DB                         basedb.IDb
	Logger                     *zap.Logger
	SignatureCollectionTimeout time.Duration `yaml:"SignatureCollectionTimeout" env:"SIGNATURE_COLLECTION_TIMEOUT" env-default:"5s" env-description:"Timeout for signature collection after consensus"`
	ETHNetwork                 *core.Network
	Network                    network.Network
	Beacon                     beacon.Beacon
	Shares                     []validatorstorage.ShareOptions `yaml:"Shares"`
	ShareEncryptionKeyProvider eth1.ShareEncryptionKeyProvider
	CleanRegistryData          bool
}

// IController interface
type IController interface {
	ListenToEth1Events(cn pubsub.SubjectChannel)
	StartValidators()
	GetValidatorsPubKeys() [][]byte
	GetValidatorsIndices() []spec.ValidatorIndex
	GetValidator(pubKey string) (*Validator, bool)
}

// Controller struct that manages all validator shares
type controller struct {
	context    context.Context
	collection validatorstorage.ICollection
	logger     *zap.Logger
	beacon     beacon.Beacon

	shareEncryptionKeyProvider eth1.ShareEncryptionKeyProvider

	validatorsMap *validatorsMap

	// indicesLock should be acquired when updating validator's indices
	indicesLock sync.RWMutex
}

// NewController creates new validator controller
func NewController(options ControllerOptions) IController {
	collection := validatorstorage.NewCollection(validatorstorage.CollectionOptions{
		DB:     options.DB,
		Logger: options.Logger,
	})

	ctrl := controller{
		collection:                 collection,
		context:                    options.Context,
		logger:                     options.Logger.With(zap.String("component", "validatorsController")),
		beacon:                     options.Beacon,
		shareEncryptionKeyProvider: options.ShareEncryptionKeyProvider,

		// locks
		indicesLock: sync.RWMutex{},

		validatorsMap: newValidatorsMap(options.Context, options.Logger, &Options{
			Context:                    options.Context,
			SignatureCollectionTimeout: options.SignatureCollectionTimeout,
			Logger:                     options.Logger,
			Network:                    options.Network,
			ETHNetwork:                 options.ETHNetwork,
			Beacon:                     options.Beacon,
			DB:                         options.DB,
		}),
	}

	if err := ctrl.initShares(options); err != nil {
		ctrl.logger.Panic("could not initialize shares", zap.Error(err))
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

// initShares initializes shares
func (c *controller) initShares(options ControllerOptions) error {
	if options.CleanRegistryData {
		if err := c.collection.CleanAllShares(); err != nil {
			return errors.Wrap(err, "failed to clean shares")
		}
		c.logger.Debug("all shares were removed")
	}

	if len(options.Shares) > 0 {
		c.collection.LoadMultipleFromConfig(options.Shares)
	}
	return nil
}

// setupValidators for each validatorShare with proper ibft wrappers
func (c *controller) setupValidators() {
	shares, err := c.collection.GetAllValidatorsShare()
	if err != nil {
		c.logger.Fatal("failed to get validators shares", zap.Error(err))
	}
	if len(shares) == 0 {
		c.logger.Info("could not find validators")
		return
	}
	c.logger.Info("starting validators setup...", zap.Int("shares count", len(shares)))
	for _, validatorShare := range shares {
		c.validatorsMap.GetOrCreateValidator(validatorShare)
	}
	c.logger.Info("setup validators done", zap.Int("map size", c.validatorsMap.Size()))
}

// StartValidators functions (queue streaming, msgQueue listen, etc)
func (c *controller) StartValidators() {
	c.setupValidators()
	errs := []error{}
	err := c.validatorsMap.ForEach(func(v *Validator) error {
		if err := v.Start(); err != nil {
			c.logger.Error("could not start validator", zap.Error(err),
				zap.String("pubkey", v.Share.PublicKey.SerializeToHexStr()))
			errs = append(errs, err)
		}
		return nil
	})
	if err != nil {
		c.logger.Error("failed to start validators", zap.Error(err))
	}
	if len(errs) > 0 {
		c.logger.Warn("failed to start all validators", zap.Int("count", len(errs)))
	}
}

// GetValidator returns a validator
func (c *controller) GetValidator(pubKey string) (*Validator, bool) {
	return c.validatorsMap.GetValidator(pubKey)
}

// GetValidatorsIndices returns a list of all the active validators indices and fetch indices for missing once (could be first time attesting or non active once)
func (c *controller) GetValidatorsIndices() []spec.ValidatorIndex {
	c.indicesLock.RLock()
	defer c.indicesLock.RUnlock()

	var indices []spec.ValidatorIndex
	var toFetch []phase0.BLSPubKey
	err := c.validatorsMap.ForEach(func(v *Validator) error {
		if v.Share.Index == nil {
			blsPubKey := phase0.BLSPubKey{}
			copy(blsPubKey[:], v.Share.PublicKey.Serialize())
			toFetch = append(toFetch, blsPubKey)
		} else {
			index := spec.ValidatorIndex(*v.Share.Index)
			indices = append(indices, index)
		}
		return nil
	})
	if err != nil {
		c.logger.Error("failed to get all validators public keys", zap.Error(err))
	}

	go c.updateIndices(toFetch) // saving missing indices to be ready for next ticker (slot)

	return indices
}

// GetValidatorsPubKeys returns a list of all the validators public keys
func (c *controller) GetValidatorsPubKeys() [][]byte {
	var pubKeys [][]byte

	err := c.validatorsMap.ForEach(func(v *Validator) error {
		pubKeys = append(pubKeys, v.Share.PublicKey.Serialize())
		return nil
	})
	if err != nil {
		c.logger.Error("failed to get all validators public keys", zap.Error(err))
	}

	return pubKeys
}

func (c *controller) handleValidatorAddedEvent(validatorAddedEvent eth1.ValidatorAddedEvent) {
	pubKey := hex.EncodeToString(validatorAddedEvent.PublicKey)
	logger := c.logger.With(zap.String("validatorPubKey", pubKey))
	logger.Debug("handles validator added event")
	// if exist and resync was not forced -> do nothing
	if _, ok := c.validatorsMap.GetValidator(pubKey); ok {
		logger.Debug("validator was loaded already")
		// TODO: handle updateValidator in the future
		return
	}
	validatorShare, err := c.createShare(validatorAddedEvent)
	if err != nil {
		logger.Error("failed to create share", zap.Error(err))
		return
	}
	foundShare, found, err := c.collection.GetValidatorShare(validatorShare.PublicKey.Serialize())
	if err != nil {
		logger.Error("could not check if validator share exits", zap.Error(err))
		return
	}
	if !found { // save share if not exist
		if err := c.collection.SaveValidatorShare(validatorShare); err != nil {
			logger.Error("failed to save validator share", zap.Error(err))
			return
		}
		logger.Debug("validator share was saved")
	} else {
		// TODO: handle updateValidator in the future
		validatorShare = foundShare
	}

	v := c.validatorsMap.GetOrCreateValidator(validatorShare)

	if err := v.Start(); err != nil {
		logger.Error("could not start validator", zap.Error(err))
	}
}

func (c *controller) updateIndices(pubkeys []spec.BLSPubKey) {
	c.indicesLock.RLock()
	defer c.indicesLock.RUnlock()

	if len(pubkeys) == 0 {
		return
	}
	c.logger.Debug("fetching indices for public keys", zap.Int("total", len(pubkeys)))
	validatorsIndexMap, err := c.beacon.GetIndices(pubkeys)
	if err != nil {
		c.logger.Error("failed to fetch indices", zap.Error(err))
		return
	}
	c.logger.Debug("returned indices from beacon", zap.Int("total", len(validatorsIndexMap)))
	for index, v := range validatorsIndexMap {
		uIndex := uint64(index)
		pubKey := hex.EncodeToString(v.Validator.PublicKey[:])
		if err := c.updateIndex(pubKey, &uIndex); err != nil {
			c.logger.Error("failed to update share index", zap.String("pubKey", pubKey))
			continue
		}
		c.beacon.ExtendIndexMap(index, v.Validator.PublicKey) // updating goClient map
		c.logger.Debug("share index has been updated", zap.String("pubKey", pubKey),
			zap.Uint64("index", uIndex))
	}
}

func (c *controller) updateIndex(pubKey string, index *uint64) error {
	if v, ok := c.validatorsMap.GetValidator(pubKey); ok {
		v.Share.Index = index
		err := c.collection.SaveValidatorShare(v.Share)
		if err != nil {
			c.logger.Error("failed to update share index", zap.String("pubKey", pubKey))
			return err
		}
	}
	return nil
}

func (c *controller) createShare(validatorAddedEvent eth1.ValidatorAddedEvent) (*validatorstorage.Share, error) {
	operatorPrivKey, found, err := c.shareEncryptionKeyProvider()
	if !found {
		return nil, errors.New("could not find operator private key")
	}
	if err != nil {
		return nil, errors.Wrap(err, "get operator private key")
	}
	operatorPubKey, err := rsaencryption.ExtractPublicKey(operatorPrivKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not extract operator public key")
	}
	validatorShare, err := ShareFromValidatorAddedEvent(validatorAddedEvent, operatorPubKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not create share from event")
	}
	return validatorShare, nil
}
