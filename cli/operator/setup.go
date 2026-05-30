package operator

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	cockroachdb "github.com/cockroachdb/pebble"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/ssvsigner"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/ssvsigner/keys/rsaencryption"
	"github.com/ssvlabs/ssv/ssvsigner/keystore"

	"github.com/ssvlabs/ssv/eth/eventhandler"
	"github.com/ssvlabs/ssv/eth/eventparser"
	"github.com/ssvlabs/ssv/eth/eventsyncer"
	"github.com/ssvlabs/ssv/eth/executionclient"
	"github.com/ssvlabs/ssv/eth/localevents"
	ibftstorage "github.com/ssvlabs/ssv/ibft/storage"
	ssv_identity "github.com/ssvlabs/ssv/identity"
	"github.com/ssvlabs/ssv/migrations"
	"github.com/ssvlabs/ssv/network"
	p2pv1 "github.com/ssvlabs/ssv/network/p2p"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log/fields"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	"github.com/ssvlabs/ssv/operator/slotticker"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/operator/validator"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/pebble"
)

func warnIfSSVAPIAddressUnset(logger *zap.Logger, address string, port int) {
	if address != "" {
		return
	}

	logger.Warn("SSV API address not configured; listening on all interfaces",
		zap.Int("port", port),
		zap.String("config_key", "SSVAPIAddress"),
		zap.String("recommended_address", "127.0.0.1"),
	)
}

func ensureNoMissingKeys(
	ctx context.Context,
	nodeStorage operatorstorage.Storage,
	operatorDataStore operatordatastore.OperatorDataStore,
	ssvSignerClient *ssvsigner.Client,
) error {
	if !operatorDataStore.OperatorIDReady() {
		return nil
	}

	shares := nodeStorage.Shares().List(
		nil,
		registrystorage.ByNotLiquidated(),
		registrystorage.ByOperatorID(operatorDataStore.GetOperatorID()),
	)
	if len(shares) == 0 {
		return nil
	}

	localKeys := make([]phase0.BLSPubKey, 0, len(shares))
	for _, share := range shares {
		localKeys = append(localKeys, phase0.BLSPubKey(share.SharePubKey))
	}

	missingKeys, err := ssvSignerClient.MissingKeys(ctx, localKeys)
	if err != nil {
		return fmt.Errorf("failed to check for missing keys: %w", err)
	}

	if len(missingKeys) > 0 {
		// >50 keys: log only the count to keep the line readable; otherwise list them.
		keysField := zap.Stringers("keys", missingKeys)
		if len(missingKeys) > 50 {
			keysField = zap.Int("count", len(missingKeys))
		}
		return startupError{err: errors.New("remote signer misses keys"), fields: []zap.Field{keysField}}
	}
	return nil
}

func privateKeyFromKeystore(privKeyFile, passwordFile string) (keys.OperatorPrivateKey, []byte, error) {
	// #nosec G304
	encryptedJSON, err := os.ReadFile(privKeyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("could not read PEM file: %w", err)
	}

	// #nosec G304
	keyStorePassword, err := os.ReadFile(passwordFile)
	if err != nil {
		return nil, nil, fmt.Errorf("could not read password file: %w", err)
	}

	operatorPrivKeyBytes, err := keystore.DecryptKeystore(encryptedJSON, string(keyStorePassword))
	if err != nil {
		return nil, nil, fmt.Errorf("could not decrypt operator private key keystore: %w", err)
	}

	operatorPrivKey, err := keys.PrivateKeyFromBytes(operatorPrivKeyBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("could not extract operator private key from bytes: %w", err)
	}

	return operatorPrivKey, operatorPrivKeyBytes, nil
}

func validateConfig(
	nodeStorage operatorstorage.Storage,
	networkName string,
	usingLocalEvents, usingSSVSigner, exporterMode bool,
) error {
	storedConfig, foundConfig, err := nodeStorage.GetConfig(nil)
	if err != nil {
		return fmt.Errorf("failed to get stored config: %w", err)
	}

	currentConfig := &operatorstorage.ConfigLock{
		NetworkName:      networkName,
		UsingLocalEvents: usingLocalEvents,
		UsingSSVSigner:   usingSSVSigner,
	}

	if foundConfig {
		if err := storedConfig.ValidateCompatibility(currentConfig, exporterMode); err != nil {
			return fmt.Errorf("incompatible config change: %w", err)
		}
	} else {
		if err := nodeStorage.SaveConfig(nil, currentConfig); err != nil {
			return fmt.Errorf("failed to store config: %w", err)
		}
	}

	return nil
}

func setupBadgerDB(
	logger *zap.Logger,
	cfg *config,
	beaconConfig *networkconfig.Beacon,
	operatorPrivKey keys.OperatorPrivateKey,
) (*badger.DB, error) {
	db, err := badger.New(logger, cfg.DBOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}

	if err := applyMigrations(logger, cfg, beaconConfig, operatorPrivKey, db, cfg.DBOptions.Path); err != nil {
		return nil, fmt.Errorf("apply migrations: %w", err)
	}

	return db, nil
}

func setupPebbleDB(
	logger *zap.Logger,
	cfg *config,
	beaconConfig *networkconfig.Beacon,
	operatorPrivKey keys.OperatorPrivateKey,
) (*pebble.DB, error) {
	dbPath := cfg.DBOptions.Path + "-pebble" // opinionated approach to avoid corrupting old db location

	db, err := pebble.New(logger, dbPath, &cockroachdb.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}

	if err := applyMigrations(logger, cfg, beaconConfig, operatorPrivKey, db, dbPath); err != nil {
		return nil, fmt.Errorf("apply migrations: %w", err)
	}

	return db, nil
}

func applyMigrations(
	logger *zap.Logger,
	cfg *config,
	beaconConfig *networkconfig.Beacon,
	operatorPrivKey keys.OperatorPrivateKey,
	db basedb.Database,
	dbPath string,
) error {
	migrationOpts := migrations.Options{
		Db:              db,
		DbPath:          dbPath,
		BeaconConfig:    beaconConfig,
		OperatorPrivKey: operatorPrivKey,
	}

	applied, err := migrations.Run(cfg.DBOptions.Ctx, logger, migrationOpts)
	if err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	if applied == 0 {
		return nil
	}

	// If migrations were applied, we run a full garbage collection cycle
	// to reclaim any space that may have been freed up.

	logger.Debug("running full GC cycle...")

	ctx, cancel := context.WithTimeout(cfg.DBOptions.Ctx, 6*time.Minute)
	defer cancel()

	start := time.Now()

	if err := db.FullGC(ctx); err != nil {
		return fmt.Errorf("failed to collect garbage: %w", err)
	}

	logger.Debug("post-migrations garbage collection completed", fields.Took(time.Since(start)))

	return nil
}

func setupOperatorDataStore(
	nodeStorage operatorstorage.Storage,
	base64PubKey string,
) (operatordatastore.OperatorDataStore, error) {
	if base64PubKey == "" {
		// Exporter runs without operator identity, so initialize an empty datastore
		// instead of looking up operator data by pubkey.
		return operatordatastore.New(&registrystorage.OperatorData{}), nil
	}

	operatorData, found, err := nodeStorage.GetOperatorDataByPubKey(nil, base64PubKey)
	if err != nil {
		return nil, fmt.Errorf("could not get operator data by public key: %w", err)
	}
	if !found {
		operatorData = &registrystorage.OperatorData{
			PublicKey: base64PubKey,
		}
	}
	if operatorData == nil {
		return nil, errors.New("invalid operator data in database: nil")
	}

	return operatordatastore.New(operatorData), nil
}

// ensureOperatorPrivateKey makes sure the operator private key hash
// is saved exactly once and never changes thereafter.
// On first run it saves the current hash; on subsequent runs it errors
// if the stored hash doesn't match either the current or legacy hash.
func ensureOperatorPrivateKey(
	nodeStorage operatorstorage.Storage,
	operatorPrivKey keys.OperatorPrivateKey,
	operatorPrivKeyPEM string,
) error {
	storedHash, found, err := nodeStorage.GetPrivateKeyHash()
	if err != nil {
		return fmt.Errorf("could not get hashed private key: %w", err)
	}

	// Current hashing method (PEM‑encoded → StorageHash)
	currentHash := operatorPrivKey.StorageHash()

	// Backwards compatibility for the old hashing method,
	// which was hashing the text from the configuration directly,
	// whereas StorageHash re-encodes with PEM format.
	cliPrivKeyDecoded, err := base64.StdEncoding.DecodeString(operatorPrivKeyPEM)
	if err != nil {
		return fmt.Errorf("could not decode private key: %w", err)
	}

	// Legacy hashing method (base64‑decoded bytes → HashKeyBytes)
	legacyHash := rsaencryption.HashKeyBytes(cliPrivKeyDecoded)

	if !found {
		// First run: persist the hash.
		if err := nodeStorage.SavePrivateKeyHash(currentHash); err != nil {
			return fmt.Errorf("could not save hashed private key: %w", err)
		}
		return nil
	}

	// Subsequent runs: enforce immutability.
	if !bytes.Equal(currentHash, storedHash) &&
		!bytes.Equal(legacyHash, storedHash) {
		// Prevent the node from running with a different key.
		return fmt.Errorf("operator private key is not matching the one encrypted the storage")
	}

	return nil
}

// ensureOperatorPubKey makes sure the operator public key is stored exactly once
// and never changes. On first run it saves the key; thereafter it returns an error
// if the stored key and the new key don't match.
func ensureOperatorPubKey(nodeStorage operatorstorage.Storage, operatorPubKeyBase64 string) error {
	storedPubKey, found, err := nodeStorage.GetPublicKey()
	if err != nil {
		return fmt.Errorf("could not get public key: %w", err)
	}

	if !found {
		// No key yet in storage → first run, so save it.
		if err := nodeStorage.SavePublicKey(operatorPubKeyBase64); err != nil {
			return fmt.Errorf("could not save public key: %w", err)
		}
		return nil
	}

	// Key already exists → enforce immutability
	if storedPubKey != operatorPubKeyBase64 {
		// Prevent the node from running with a different key.
		return fmt.Errorf("operator public key is not matching the one in the storage")
	}

	// Everything matches
	return nil
}

func setupSSVNetwork(logger *zap.Logger, cfg *config) (*networkconfig.SSV, error) {
	var ssvConfig *networkconfig.SSV

	if cfg.SSVOptions.CustomNetwork != nil {
		ssvConfig = cfg.SSVOptions.CustomNetwork
		logger.Info("using custom network config")
	} else if cfg.SSVOptions.NetworkName != "" {
		snc, err := networkconfig.SSVConfigByName(cfg.SSVOptions.NetworkName)
		if err != nil {
			return ssvConfig, err
		}
		ssvConfig = snc
		logger.Info("found network config by name",
			zap.String("name", cfg.SSVOptions.NetworkName),
		)
	}

	if cfg.SSVOptions.CustomDomainType != "" {
		if !strings.HasPrefix(cfg.SSVOptions.CustomDomainType, "0x") {
			return nil, errors.New("custom domain type must be a hex string")
		}
		domainBytes, err := hex.DecodeString(cfg.SSVOptions.CustomDomainType[2:])
		if err != nil {
			return nil, fmt.Errorf("failed to decode custom domain type: %w", err)
		}
		if len(domainBytes) != 4 {
			return nil, errors.New("custom domain type must be 4 bytes")
		}

		// https://github.com/ssvlabs/ssv/pull/1808 incremented the post-fork domain type by 1, so we have to maintain the compatibility.
		postForkDomain := binary.BigEndian.Uint32(domainBytes) + 1
		binary.BigEndian.PutUint32(ssvConfig.DomainType[:], postForkDomain)

		logger.Warn("running with custom domain type; it's deprecated, consider using custom network instead",
			fields.Domain(ssvConfig.DomainType),
		)
	}

	nodeType := "light"
	if cfg.SSVOptions.ValidatorOptions.FullNode {
		nodeType = "full"
	}

	logger.Info("setting ssv network",
		zap.Any("config", ssvConfig),
		zap.String("nodeType", nodeType),
		zap.String("registryContract", ssvConfig.RegistryContractAddr.String()),
	)

	return ssvConfig, nil
}

func setupP2P(ctx context.Context, logger *zap.Logger, cfg *config, db basedb.Database, exporterEnabled bool, operatorPrivKey keys.OperatorPrivateKey, signerClient *ssvsigner.Client) (network.P2PNetwork, error) {
	_, unprotectFn, err := decideNetworkKeyProtectors(ctx, logger, db, exporterEnabled, operatorPrivKey, signerClient)
	if err != nil {
		return nil, fmt.Errorf("failed to decide p2p network key protection: %w", err)
	}

	istore := ssv_identity.NewIdentityStore(logger, db, unprotectFn)
	netPrivKey, err := istore.SetupNetworkKey(ctx, cfg.NetworkPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to setup network private key: %w", err)
	}
	cfg.P2pNetworkConfig.NetworkPrivateKey = netPrivKey

	n, err := p2pv1.New(logger, &cfg.P2pNetworkConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to setup p2p network: %w", err)
	}
	return n, nil
}

func decideNetworkKeyProtectors(
	ctx context.Context,
	logger *zap.Logger,
	db basedb.Database,
	exporterEnabled bool,
	operatorPrivKey keys.OperatorPrivateKey,
	signerClient *ssvsigner.Client,
) (
	func(context.Context, []byte) ([]byte, error),
	func(context.Context, []byte) ([]byte, error),
	error,
) {
	if exporterEnabled {
		return nil, nil, nil
	}

	if operatorPrivKey != nil {
		encryptionKey, err := operatorPrivKey.EKMEncryptionKey()
		if err != nil {
			return nil, nil, fmt.Errorf("derive operator-based network key protection secret: %w", err)
		}
		protectFn := func(_ context.Context, plaintext []byte) ([]byte, error) {
			return keys.EncryptPayload(encryptionKey, plaintext)
		}
		unprotectFn := func(_ context.Context, protectedValue []byte) ([]byte, error) {
			return keys.DecryptPayload(encryptionKey, protectedValue)
		}
		return protectFn, unprotectFn, nil
	}

	if signerClient != nil {
		err := probeRemoteNetworkKeyProtector(ctx, signerClient)
		if err == nil {
			return signerClient.OperatorEncrypt, signerClient.OperatorDecrypt, nil
		}
		if !errors.Is(err, ssvsigner.ErrOperatorDataProtectionUnsupported) {
			return nil, nil, fmt.Errorf("probe ssv-signer p2p network key protection: %w", err)
		}

		hasEncryptedKey, hasEncryptedKeyErr := ssv_identity.HasEncryptedNetworkKey(db)
		if hasEncryptedKeyErr != nil {
			return nil, nil, fmt.Errorf("inspect stored p2p network private key format: %w", hasEncryptedKeyErr)
		}
		if hasEncryptedKey {
			return nil, nil, fmt.Errorf("existing database contains an encrypted p2p network private key, but the configured ssv-signer cannot encrypt or decrypt it. Upgrade ssv-signer to a version that supports /v1/operator/encrypt and /v1/operator/decrypt, or restore the operator key and signing mode that originally encrypted this database: %w", err)
		}

		logger.Warn("ssv-signer does not support remote p2p network key protection, falling back to local compatibility mode",
			zap.Error(err),
		)
	}

	return nil, nil, nil
}

func probeRemoteNetworkKeyProtector(
	ctx context.Context,
	client *ssvsigner.Client,
) error {
	probeKey := make([]byte, 32)
	if _, err := crand.Read(probeKey); err != nil {
		return fmt.Errorf("generate remote data protector probe: %w", err)
	}
	encrypted, err := client.OperatorEncrypt(ctx, probeKey)
	if err != nil {
		return fmt.Errorf("probe remote data protector encrypt: %w", err)
	}
	decrypted, err := client.OperatorDecrypt(ctx, encrypted)
	if err != nil {
		return fmt.Errorf("probe remote data protector decrypt: %w", err)
	}
	if !bytes.Equal(decrypted, probeKey) {
		return errors.New("probe remote network key protector mismatch")
	}
	return nil
}

// syncContractEvents blocks until historical events are synced and then spawns a goroutine syncing ongoing events.
func syncContractEvents(
	ctx context.Context,
	logger *zap.Logger,
	cfg *config,
	executionClient executionclient.Provider,
	validatorCtrl *validator.Controller,
	networkConfig *networkconfig.Network,
	nodeStorage operatorstorage.Storage,
	operatorDataStore operatordatastore.OperatorDataStore,
	keyManager ekm.KeyManager,
	doppelgangerHandler eventhandler.DoppelgangerProvider,
) (*eventsyncer.EventSyncer, error) {
	eventFilterer, err := executionClient.Filterer()
	if err != nil {
		return nil, fmt.Errorf("failed to set up event filterer: %w", err)
	}

	eventParser, err := eventparser.New(eventFilterer)
	if err != nil {
		return nil, fmt.Errorf("failed to create event parser: %w", err)
	}

	eventHandler, err := eventhandler.New(
		nodeStorage,
		eventParser,
		validatorCtrl,
		networkConfig,
		operatorDataStore,
		keyManager,
		doppelgangerHandler,
		eventhandler.WithFullNode(),
		eventhandler.WithLogger(logger),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to setup event data handler: %w", err)
	}

	eventSyncer := eventsyncer.New(
		nodeStorage,
		executionClient,
		eventHandler,
		eventsyncer.WithLogger(logger),
	)

	fromBlock, found, err := nodeStorage.GetLastProcessedBlock(nil)
	if err != nil {
		return nil, fmt.Errorf("syncing registry contract events failed, could not get last processed block: %w", err)
	}
	if !found {
		fromBlock = networkConfig.RegistrySyncOffset
	} else if fromBlock == nil {
		return nil, errors.New("syncing registry contract events failed, last processed block is nil")
	} else {
		// Start syncing from the next block.
		fromBlock = new(big.Int).SetUint64(fromBlock.Uint64() + 1)
	}

	// load & parse local events yaml if exists, otherwise sync from contract
	if len(cfg.LocalEventsPath) != 0 {
		localEvents, err := localevents.Load(cfg.LocalEventsPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load local events: %w", err)
		}

		if err := eventHandler.HandleLocalEvents(ctx, localEvents); err != nil {
			return nil, fmt.Errorf("error occurred while running event data handler: %w", err)
		}
	} else {
		// Sync historical registry events.
		logger.Debug("syncing historical registry events", zap.Uint64("fromBlock", fromBlock.Uint64()))
		lastProcessedBlock, err := eventSyncer.SyncHistory(ctx, fromBlock.Uint64())
		switch {
		case errors.Is(err, executionclient.ErrNothingToSync):
			// Nothing was synced, keep fromBlock as is.
			logger.Info("finished syncing historical events, nothing to sync",
				zap.Uint64("from_block", fromBlock.Uint64()),
			)
		case err == nil:
			// Successfully synced up to a fresh block, advance fromBlock to the block after lastProcessedBlock.
			logger.Info("finished syncing historical events to a fresh block",
				zap.Uint64("from_block", fromBlock.Uint64()),
				zap.Uint64("last_processed_block", lastProcessedBlock),
			)
			fromBlock = new(big.Int).SetUint64(lastProcessedBlock + 1)
		default:
			return nil, fmt.Errorf("failed to sync historical registry events: %w", err)
		}

		// Print registry stats.
		shares := nodeStorage.Shares().List(nil)
		operators, err := nodeStorage.ListOperatorsAll(nil)
		if err != nil {
			logger.Error("failed to get operators", zap.Error(err))
		}

		operatorValidators := 0
		liquidatedValidators := 0
		operatorID := operatorDataStore.GetOperatorID()
		if operatorDataStore.OperatorIDReady() {
			for _, share := range shares {
				if share.BelongsToOperator(operatorID) {
					operatorValidators++
				}
				if share.Liquidated {
					liquidatedValidators++
				}
			}
		}
		logger.Info("historical registry sync stats",
			zap.Uint64("my_operator_id", operatorID),
			zap.Int("operators", len(operators)),
			zap.Int("validators", len(shares)),
			zap.Int("liquidated_validators", liquidatedValidators),
			zap.Int("my_validators", operatorValidators),
		)

		// Sync ongoing registry events in the background, crash if ongoing sync has stopped because
		// the SSV node cannot work without being up to date with Ethereum events.
		// When block ordering looks wrong, stop the node instead of continuing
		// with possibly incorrect event state. Until reorg handling exists,
		// restart from persisted state is safer than guessing in-process.
		go func() {
			err := eventSyncer.SyncOngoing(ctx, fromBlock.Uint64())
			if err != nil && !errors.Is(err, context.Canceled) {
				logger.Fatal("failed syncing ongoing registry events",
					zap.Uint64("last_processed_block", lastProcessedBlock),
					zap.Error(err),
				)
			}
		}()
	}

	return eventSyncer, nil
}

func initSlotPruning(ctx context.Context, stores *ibftstorage.ParticipantStores, slotTickerProvider slotticker.Provider, slot phase0.Slot, retain uint64) {
	var wg sync.WaitGroup

	threshold := slot - phase0.Slot(retain)

	// async perform initial slot gc
	_ = stores.Each(func(_ spectypes.BeaconRole, store ibftstorage.ParticipantStore) error {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.Prune(ctx, threshold)
		}()
		return nil
	})

	wg.Wait()

	// start background job for removing old slots on every tick
	_ = stores.Each(func(_ spectypes.BeaconRole, store ibftstorage.ParticipantStore) error {
		go store.PruneContinuously(ctx, slotTickerProvider, phase0.Slot(retain))
		return nil
	})
}
