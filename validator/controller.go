package validator

import (
	"context"
	"encoding/hex"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/pubsub"
	"github.com/bloxapp/ssv/storage/basedb"
	validatorstorage "github.com/bloxapp/ssv/validator/storage"
	"github.com/pkg/errors"
	"go.uber.org/zap"
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

// IController represent the validators controller,
// it takes care of bootstrapping, updating and managing existing validators and their shares
type IController interface {
	ListenToEth1Events(cn pubsub.SubjectChannel)
	ProcessEth1Event(e eth1.Event) error
	StartValidators()
	GetValidatorsIndices() []spec.ValidatorIndex
	GetValidator(pubKey string) (*Validator, bool)
}

// controller implements IController
type controller struct {
	context    context.Context
	collection validatorstorage.ICollection
	logger     *zap.Logger
	beacon     beacon.Beacon

	shareEncryptionKeyProvider eth1.ShareEncryptionKeyProvider

	validatorsMap *validatorsMap
}

// NewController creates a new validator controller instance
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

// ProcessEth1Event handles a single event, will be called in both sync and stream events from registry contract
func (c *controller) ProcessEth1Event(e eth1.Event) error {
	if validatorAddedEvent, ok := e.Data.(eth1.ValidatorAddedEvent); ok {
		pubKey := hex.EncodeToString(validatorAddedEvent.PublicKey)
		if err := c.handleValidatorAddedEvent(validatorAddedEvent); err != nil {
			c.logger.Error("could not process validator",
				zap.String("pubkey", pubKey), zap.Error(err))
			return err
		}
	}
	return nil
}

// initShares initializes shares, should be called upon creation of controller
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

// StartValidators loads all persisted shares nd setup the corresponding validators
func (c *controller) StartValidators() {
	shares, err := c.collection.GetAllValidatorsShare()
	if err != nil {
		c.logger.Fatal("failed to get validators shares", zap.Error(err))
	}
	if len(shares) == 0 {
		c.logger.Info("could not find validators")
		return
	}
	c.setupValidators(shares)
}

// setupValidators setup and starts validators from the given shares
func (c *controller) setupValidators(shares []*validatorstorage.Share) {
	c.logger.Info("starting validators setup...", zap.Int("shares count", len(shares)))
	var errs []error
	var fetchMetadata [][]byte
	for _, validatorShare := range shares {
		v := c.validatorsMap.GetOrCreateValidator(validatorShare)
		pk := v.Share.PublicKey.SerializeToHexStr()
		logger := c.logger.With(zap.String("pubkey", pk))
		if !v.Share.HasMetadata() { // fetching index and status in case not exist
			fetchMetadata = append(fetchMetadata, v.Share.PublicKey.Serialize())
			logger.Warn("could not start validator as metadata not found")
			continue
		}
		if err := c.startValidator(v); err != nil {
			logger.Warn("could not start validator", zap.Error(err))
			errs = append(errs, err)
		}
	}
	c.logger.Info("setup validators done", zap.Int("map size", c.validatorsMap.Size()),
		zap.Int("failures", len(errs)), zap.Int("missing metadata", len(fetchMetadata)),
		zap.Int("shares count", len(shares)))

	go c.updateValidatorsMetadata(fetchMetadata)
}

// updateValidatorsMetadata updates metadata of the given public keys
func (c *controller) updateValidatorsMetadata(pubKeys [][]byte) {
	if len(pubKeys) > 0 {
		c.logger.Debug("updating validators", zap.Int("count", len(pubKeys)))
		onUpdated := func(pk string, meta *beacon.ValidatorMetadata) {
			reportValidatorStatus(pk, meta, c.logger)
		}
		if err := beacon.UpdateValidatorsMetadata(pubKeys, c, c.beacon, onUpdated); err != nil {
			c.logger.Error("could not update all validators", zap.Error(err))
		}
	}
}

// UpdateValidatorMetadata updates a given validator with metadata (implements ValidatorMetadataStorage)
func (c *controller) UpdateValidatorMetadata(pk string, metadata *beacon.ValidatorMetadata) error {
	if metadata == nil {
		return errors.New("could not update empty metadata")
	}
	if v, found := c.validatorsMap.GetValidator(pk); found {
		v.Share.Metadata = metadata
		if err := c.collection.SaveValidatorShare(v.Share); err != nil {
			return err
		}
		if err := c.startValidator(v); err != nil {
			c.logger.Error("could not start validator", zap.Error(err))
		}
	}
	return nil
}

// GetValidator returns a validator instance
func (c *controller) GetValidator(pubKey string) (*Validator, bool) {
	return c.validatorsMap.GetValidator(pubKey)
}

// GetValidatorsIndices returns a list of all the active validators indices
// and fetch indices for missing once (could be first time attesting or non active once)
func (c *controller) GetValidatorsIndices() []spec.ValidatorIndex {
	var toFetch [][]byte
	var indices []spec.ValidatorIndex
	err := c.validatorsMap.ForEach(func(v *Validator) error {
		logger := c.logger.With(zap.String("pubKey", v.Share.PublicKey.SerializeToHexStr()))
		if !v.Share.HasMetadata() {
			logger.Warn("validator share doesn't have an index")
			toFetch = append(toFetch, v.Share.PublicKey.Serialize())
		} else {
			indices = append(indices, v.Share.Metadata.Index)
		}
		return nil
	})
	if err != nil {
		c.logger.Error("failed to get all validators public keys", zap.Error(err))
	}

	go c.updateValidatorsMetadata(toFetch)

	return indices
}

// handleValidatorAddedEvent handles registry contract event for validator added
func (c *controller) handleValidatorAddedEvent(validatorAddedEvent eth1.ValidatorAddedEvent) error {
	pubKey := hex.EncodeToString(validatorAddedEvent.PublicKey[:])
	logger := c.logger.With(zap.String("pubKey", pubKey))
	logger.Debug("handles validator added event")
	// if exist -> do nothing
	if _, ok := c.validatorsMap.GetValidator(pubKey); ok {
		logger.Debug("validator was loaded already")
		// TODO: handle updateValidator in the future
		return nil
	}
	metricsValidatorStatus.WithLabelValues(pubKey).Set(float64(validatorStatusInactive))
	validatorShare, found, err := c.collection.GetValidatorShare(validatorAddedEvent.PublicKey[:])
	if err != nil {
		return errors.Wrap(err, "could not check if validator share exits")
	}
	if !found {
		validatorShare, err = createShareWithOperatorKey(validatorAddedEvent, c.shareEncryptionKeyProvider)
		if err != nil {
			return errors.Wrap(err, "failed to create share")
		}
		if err := c.onNewShare(validatorShare); err != nil {
			return err
		}
	}

	v := c.validatorsMap.GetOrCreateValidator(validatorShare)

	if err := c.startValidator(v); err != nil {
		logger.Warn("could not start validator", zap.Error(err))
	}

	return nil
}

// onNewShare is called when a new validator was added or during registry sync
// if the validator was persisted already, this function won't be called
func (c *controller) onNewShare(share *validatorstorage.Share) error {
	logger := c.logger.With(zap.String("pubKey", share.PublicKey.SerializeToHexStr()))
	if updated, err := updateShareMetadata(share, c.beacon); err != nil {
		logger.Warn("could not add validator metadata", zap.Error(err))
	} else if !updated {
		logger.Warn("could not find validator metadata")
	} else {
		logger.Debug("validator metadata was updated",
			zap.Uint64("index", uint64(share.Metadata.Index)))
		reportValidatorStatus(share.PublicKey.SerializeToHexStr(), share.Metadata, c.logger)
	}
	if err := c.collection.SaveValidatorShare(share); err != nil {
		return errors.Wrap(err, "failed to save new share")
	}
	logger.Debug("new validator share was saved")
	return nil
}

// startValidator will start the given validator if applicable
func (c *controller) startValidator(v *Validator) error {
	reportValidatorStatus(v.Share.PublicKey.SerializeToHexStr(), v.Share.Metadata, c.logger)
	if !v.Share.HasMetadata() {
		return errors.New("could not start validator: metadata not found")
	}
	if v.Share.Metadata.Index == 0 {
		return errors.New("could not start validator: index not found")
	}
	if err := v.Start(); err != nil {
		metricsValidatorStatus.WithLabelValues(v.Share.PublicKey.SerializeToHexStr()).Set(float64(validatorStatusError))
		return errors.Wrap(err, "could not start validator")
	}
	return nil
}
