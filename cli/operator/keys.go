package operator

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ekmadapter"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log/fields"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/ssvsigner"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/ssvsigner/keys/rsaencryption"
	"github.com/ssvlabs/ssv/ssvsigner/keystore"
	ssvsignertls "github.com/ssvlabs/ssv/ssvsigner/tls"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// The remote signer may be briefly unavailable when the missing-keys check runs —
// most commonly when the whole stack (node, ssv-signer, web3signer) restarts
// together and web3signer is still starting or loading keys — so the check retries
// with backoff for a bounded window instead of failing startup on the first error.
const (
	missingKeysRetryWindow   = 2 * time.Minute
	missingKeysRetryDelay    = time.Second
	missingKeysRetryMaxDelay = 16 * time.Second
)

func ensureNoMissingKeys(
	ctx context.Context,
	logger *zap.Logger,
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

	missingKeys, err := fetchMissingKeysWithRetry(ctx, logger, ssvSignerClient, localKeys, missingKeysRetryWindow, missingKeysRetryDelay)
	if err != nil {
		return fmt.Errorf("failed to check for missing keys: %w", err)
	}

	if len(missingKeys) > 0 {
		// >50 keys: log only the count to keep the line readable; otherwise list them.
		keysField := zap.Stringers("keys", missingKeys)
		if len(missingKeys) > 50 {
			keysField = zap.Int("count", len(missingKeys))
		}
		return startupError{err: fmt.Errorf("remote signer misses keys"), fields: []zap.Field{keysField}}
	}
	return nil
}

// fetchMissingKeysWithRetry calls MissingKeys on the remote signer, retrying failed
// attempts with exponential backoff until the window elapses or ctx is canceled.
// The window bounds when attempts may start, not the total duration: an in-flight
// attempt is never cut short (each is already bounded by the client's request
// timeout), so the final one may finish up to that timeout past the window. A hard
// cutoff would only clip the last attempt and mask its underlying error with a
// context error.
// All errors are retried: transient ones (the signer stack still starting up)
// resolve within the window, and persistent ones only postpone the startup failure
// by roughly the window.
func fetchMissingKeysWithRetry(
	ctx context.Context,
	logger *zap.Logger,
	ssvSignerClient *ssvsigner.Client,
	localKeys []phase0.BLSPubKey,
	window time.Duration,
	delay time.Duration,
) ([]phase0.BLSPubKey, error) {
	deadline := time.Now().Add(window)
	for {
		missingKeys, err := ssvSignerClient.MissingKeys(ctx, localKeys)
		if err == nil {
			return missingKeys, nil
		}

		// Give up if ctx is done or the next attempt wouldn't start within the window.
		if ctx.Err() != nil || time.Now().Add(delay).After(deadline) {
			return nil, err
		}

		logger.Warn("failed to check for missing keys in remote signer, retrying",
			zap.Duration("retry_in", delay),
			zap.Error(err))

		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(delay):
		}

		delay = min(delay*2, missingKeysRetryMaxDelay)
	}
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

// ensureOperatorPrivateKey makes sure the operator private key hash is saved exactly once and
// never changes thereafter. On first run it saves the current hash; on subsequent runs it errors
// if the stored hash matches neither the current nor the legacy hash.
func ensureOperatorPrivateKey(
	nodeStorage operatorstorage.Storage,
	operatorPrivKey keys.OperatorPrivateKey,
	operatorPrivKeyPEM string,
) error {
	storedHash, found, err := nodeStorage.GetPrivateKeyHash()
	if err != nil {
		return fmt.Errorf("could not get hashed private key: %w", err)
	}

	// Current hashing method (PEM-encoded → StorageHash)
	currentHash := operatorPrivKey.StorageHash()

	// Backwards compatibility for the old hashing method,
	// which was hashing the text from the configuration directly,
	// whereas StorageHash re-encodes with PEM format.
	cliPrivKeyDecoded, err := base64.StdEncoding.DecodeString(operatorPrivKeyPEM)
	if err != nil {
		return fmt.Errorf("could not decode private key: %w", err)
	}

	// Legacy hashing method (base64-decoded bytes → HashKeyBytes)
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
		return fmt.Errorf("operator private key does not match the one that encrypted the storage")
	}

	return nil
}

// ensureOperatorPubKey makes sure the operator public key is stored exactly once and never
// changes thereafter. On first run it saves the key; on subsequent runs it errors if the stored
// key doesn't match the new one.
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
		return fmt.Errorf("operator public key does not match the one in the storage")
	}

	return nil
}

// operatorIdentity is the operator's signing material resolved from config. In exporter mode it is
// empty (no signing); otherwise it carries either the private key (keystore / arg modes) or the
// ssv-signer client (remote mode), plus the base64 public key persisted on first run.
type operatorIdentity struct {
	privKey    keys.OperatorPrivateKey
	privKeyPEM string
	ssvSigner  *ssvsigner.Client
	pubKeyB64  string
}

// resolveOperatorIdentity loads the operator signing material for the node's mode: exporter nodes
// don't sign (empty identity); operator nodes resolve it from ssv-signer, a keystore, or a raw
// private key, depending on the configured signing method.
func resolveOperatorIdentity(ctx context.Context, logger *zap.Logger, cfg *config, res resolved) (operatorIdentity, error) {
	if res.isExporter() {
		logger.Info("exporter mode: skipping operator signing and key manager services")
		return operatorIdentity{}, nil
	}

	if res.usingSSVSigner {
		endpointField := zap.String("ssv_signer_endpoint", cfg.SSVSigner.Endpoint)
		logger := logger.With(endpointField)
		logger.Info("using ssv-signer for signing")

		if _, err := url.ParseRequestURI(cfg.SSVSigner.Endpoint); err != nil {
			return operatorIdentity{}, startupError{err: fmt.Errorf("invalid ssv signer endpoint format: %w", err), fields: []zap.Field{endpointField}}
		}

		ssvSignerOptions := []ssvsigner.ClientOption{
			ssvsigner.WithLogger(logger),
			ssvsigner.WithRequestTimeout(cfg.SSVSigner.RequestTimeout),
		}

		if cfg.SSVSigner.KeystoreFile != "" || cfg.SSVSigner.ServerCertFile != "" {
			tlsConfig := &ssvsignertls.Config{
				ClientKeystoreFile:         cfg.SSVSigner.KeystoreFile,
				ClientKeystorePasswordFile: cfg.SSVSigner.KeystorePasswordFile,
				ClientServerCertFile:       cfg.SSVSigner.ServerCertFile,
			}

			clientConfig, err := tlsConfig.LoadClientTLSConfig()
			if err != nil {
				return operatorIdentity{}, startupError{err: fmt.Errorf("failed to load ssv-signer TLS config: %w", err), fields: []zap.Field{endpointField}}
			}

			ssvSignerOptions = append(ssvSignerOptions, ssvsigner.WithTLSConfig(clientConfig))
		}

		ssvSignerClient := ssvsigner.NewClient(
			cfg.SSVSigner.Endpoint,
			ssvSignerOptions...,
		)

		operatorPubKeyString, err := ssvSignerClient.OperatorIdentity(ctx)
		if err != nil {
			return operatorIdentity{}, startupError{err: fmt.Errorf("ssv-signer unavailable: %w", err), fields: []zap.Field{endpointField}}
		}

		pubKeyField := zap.String(fields.FieldPubKey, operatorPubKeyString)
		logger = logger.With(pubKeyField)
		logger.Info("ssv-signer operator identity")

		operatorPubKey, err := keys.PublicKeyFromString(operatorPubKeyString)
		if err != nil {
			return operatorIdentity{}, startupError{err: fmt.Errorf("could not extract operator public key from string: %w", err), fields: []zap.Field{endpointField, pubKeyField}}
		}

		operatorPubKeyBase64, err := operatorPubKey.Base64()
		if err != nil {
			return operatorIdentity{}, startupError{err: fmt.Errorf("could not get operator public key base64: %w", err), fields: []zap.Field{endpointField, pubKeyField}}
		}

		return operatorIdentity{ssvSigner: ssvSignerClient, pubKeyB64: operatorPubKeyBase64}, nil
	}

	var operatorPrivKey keys.OperatorPrivateKey
	var operatorPrivKeyPEM string
	if res.usingKeystore {
		logger.Info("getting operator private key from keystore")

		privKey, decryptedKeystore, err := privateKeyFromKeystore(cfg.KeyStore.PrivateKeyFile, cfg.KeyStore.PasswordFile)
		if err != nil {
			return operatorIdentity{}, fmt.Errorf("could not extract private key from keystore: %w", err)
		}

		operatorPrivKey = privKey
		operatorPrivKeyPEM = base64.StdEncoding.EncodeToString(decryptedKeystore)
	} else if res.usingPrivKey {
		logger.Info("getting operator private key from args")

		privKey, err := keys.PrivateKeyFromString(cfg.OperatorPrivateKey)
		if err != nil {
			return operatorIdentity{}, fmt.Errorf("could not decode operator private key: %w", err)
		}

		operatorPrivKey = privKey
		operatorPrivKeyPEM = cfg.OperatorPrivateKey
	}

	operatorPubKeyBase64, err := operatorPrivKey.Public().Base64()
	if err != nil {
		return operatorIdentity{}, fmt.Errorf("could not get operator public key base64: %w", err)
	}

	return operatorIdentity{privKey: operatorPrivKey, privKeyPEM: operatorPrivKeyPEM, pubKeyB64: operatorPubKeyBase64}, nil
}

// buildKeyManager constructs the operator's key manager and signer. Exporter nodes don't sign, so
// it returns (nil, nil); operator nodes get a remote key manager (ssv-signer) or a local one.
func buildKeyManager(
	ctx context.Context,
	logger *zap.Logger,
	cfg *config,
	res resolved,
	beaconConfig *networkconfig.Beacon,
	db basedb.Database,
	ssvSignerClient *ssvsigner.Client,
	operatorPrivKey keys.OperatorPrivateKey,
	operatorDataStore operatordatastore.OperatorDataStore,
) (ekm.KeyManager, types.OperatorSigner, error) {
	if res.isExporter() {
		return nil, nil, nil
	}

	ekmDB := ekmadapter.NewDatabaseAdapter(db)
	if res.usingSSVSigner {
		remoteKeyManager, err := ekm.NewRemoteKeyManager(
			ctx,
			logger,
			beaconConfig,
			ssvSignerClient,
			ekmDB,
			operatorDataStore.GetOperatorID,
		)
		if err != nil {
			return nil, nil, fmt.Errorf("could not create remote key manager: %w", err)
		}

		return remoteKeyManager, remoteKeyManager, nil
	}

	localKeyManager, err := ekm.NewLocalKeyManager(logger, ekmDB, beaconConfig, operatorPrivKey)
	if err != nil {
		return nil, nil, fmt.Errorf("could not create new eth-key-manager signer: %w", err)
	}

	return localKeyManager, types.NewSsvOperatorSigner(operatorPrivKey, operatorDataStore.GetOperatorID), nil
}
