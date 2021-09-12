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
	"strings"
	"time"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
)

var (
	errIndicesNotFound = errors.New("indices not found")
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
	ProcessEth1Event(e eth1.Event) error
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
			if err := c.ProcessEth1Event(event); err != nil {
				c.logger.Error("could not process event", zap.Error(err))
			}
		}
	}
}

// ProcessEth1Event handles a single event
func (c *controller) ProcessEth1Event(e eth1.Event) error {
	if validatorAddedEvent, ok := e.Data.(eth1.ValidatorAddedEvent); ok {
		if err := c.handleValidatorAddedEvent(validatorAddedEvent); err != nil {
			logger := c.logger.With(zap.String("pubkey", hex.EncodeToString(validatorAddedEvent.PublicKey)),
				zap.Error(err))
			if strings.Contains(err.Error(), errIndicesNotFound.Error()) {
				logger.Warn("indices not found, please check validator")
				return nil
			}
			logger.Error("could not process validator")
			return err
		}
	}
	return nil
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
	var indices []spec.ValidatorIndex
	err := c.validatorsMap.ForEach(func(v *Validator) error {
		if v.Share.Index == nil {
			c.logger.Error("validator share doesn't have an index",
				zap.String("pubKey", v.Share.PublicKey.SerializeToHexStr()))
		} else {
			index := spec.ValidatorIndex(*v.Share.Index)
			indices = append(indices, index)
		}
		return nil
	})
	if err != nil {
		c.logger.Error("failed to get all validators public keys", zap.Error(err))
	}

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

func (c *controller) handleValidatorAddedEvent(validatorAddedEvent eth1.ValidatorAddedEvent) error {
	pubKey := hex.EncodeToString(validatorAddedEvent.PublicKey)
	logger := c.logger.With(zap.String("validatorPubKey", pubKey))
	logger.Debug("handles validator added event")
	// if exist and resync was not forced -> do nothing
	if _, ok := c.validatorsMap.GetValidator(pubKey); ok {
		logger.Debug("validator was loaded already")
		// TODO: handle updateValidator in the future
		return nil
	}
	validatorShare, err := c.createShare(validatorAddedEvent)
	if err != nil {
		return errors.Wrap(err, "failed to create share")
	}
	foundShare, found, err := c.collection.GetValidatorShare(validatorShare.PublicKey.Serialize())
	if err != nil {
		return errors.Wrap(err, "could not check if validator share exits")
	}
	if !found {
		if err := c.addValidatorIndex(validatorShare); err != nil {
			return errors.Wrap(err, "failed to add validator index")
		}
		if err := c.collection.SaveValidatorShare(validatorShare); err != nil {
			return errors.Wrap(err, "failed to save new share")
		}
		logger.Debug("new validator share was saved")
	} else {
		// TODO: handle updateValidator in the future
		validatorShare = foundShare
	}

	v := c.validatorsMap.GetOrCreateValidator(validatorShare)

	if err := v.Start(); err != nil {
		return errors.Wrap(err, "could not start validator")
	}

	logger.Debug("new validator was added and started successfully")

	return nil
}

// addValidatorIndex is called whenever a new validator is added (and it was not persist yet)
// validator's index is fetched as part of this method
func (c *controller) addValidatorIndex(validatorShare *validatorstorage.Share) error {
	pubKey := validatorShare.PublicKey.SerializeToHexStr()
	indices, err := c.fetchIndices([][]byte{validatorShare.PublicKey.Serialize()})
	if err != nil {
		return errors.Wrap(err, "failed to fetch indices")
	}
	index, ok := indices[pubKey]
	if !ok {
		return errIndicesNotFound
	}
	validatorShare.Index = &index
	return nil
}

func (c *controller) fetchIndices(sharesPubKeys [][]byte) (map[string]uint64, error) {
	if len(sharesPubKeys) == 0 {
		return nil, errors.New("invalid pubkeys. at least one validator is required")
	}
	var pubkeys []phase0.BLSPubKey
	for _, pk := range sharesPubKeys {
		blsPubKey := phase0.BLSPubKey{}
		copy(blsPubKey[:], pk[:])
		pubkeys = append(pubkeys, blsPubKey)
	}
	c.logger.Debug("fetching indices for public keys", zap.Int("total", len(pubkeys)),
		zap.Any("pubkeys", pubkeys))
	validatorsIndexMap, err := c.beacon.GetIndices(pubkeys)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get indices from beacon")
	}
	indices := map[string]uint64{}
	for index, v := range validatorsIndexMap {
		pk := hex.EncodeToString(v.Validator.PublicKey[:])
		indices[pk] = uint64(index)
		c.beacon.ExtendIndexMap(index, v.Validator.PublicKey) // updating goClient map
	}
	c.logger.Debug("fetched indices from beacon", zap.Any("indices", indices))
	return indices, nil
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
