package operator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
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

// retryBackoff configures fetchMissingKeysWithRetry's bounded exponential backoff.
type retryBackoff struct {
	window   time.Duration // span in which new attempts may start
	delay    time.Duration // wait before the first retry, doubled after each failure
	maxDelay time.Duration // ceiling for the doubling delay
}

// The remote signer may be briefly unavailable when the missing-keys check runs —
// most commonly when the whole stack (node, ssv-signer, web3signer) restarts
// together and web3signer is still starting or loading keys — so the check retries
// with backoff for a bounded window instead of failing startup on the first error.
var missingKeysRetryBackoff = retryBackoff{
	window:   2 * time.Minute,
	delay:    time.Second,
	maxDelay: 16 * time.Second,
}

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

	missingKeys, err := fetchMissingKeysWithRetry(ctx, logger, ssvSignerClient, localKeys, missingKeysRetryBackoff)
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
//
// The window bounds when attempts may start, not the total duration: an in-flight
// attempt is never cut short (it is already bounded by the client's request
// timeout), so the last one may finish up to that timeout past the window — a hard
// cutoff would only clip it and mask its error with a context error.
//
// All errors are retried: transient ones (the signer stack still starting up)
// resolve within the window; persistent ones only postpone the startup failure by
// roughly the window.
func fetchMissingKeysWithRetry(
	ctx context.Context,
	logger *zap.Logger,
	ssvSignerClient *ssvsigner.Client,
	localKeys []phase0.BLSPubKey,
	backoff retryBackoff,
) ([]phase0.BLSPubKey, error) {
	deadline := time.Now().Add(backoff.window)
	delay := backoff.delay
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

		delay = min(delay*2, backoff.maxDelay)
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

// operatorKeyCheckValue identifies the operator key in the database. It's derived from the public
// key.
func operatorKeyCheckValue(operatorPubKeyBase64 string) []byte {
	checkValue := sha256.Sum256([]byte("ssv/operator-key-check/v1" + operatorPubKeyBase64))
	return checkValue[:]
}

// errOperatorKeyMismatch refuses a database that belongs to a different operator key.
var errOperatorKeyMismatch = errors.New("operator private key does not match the one that encrypted the storage")

// ensureOperatorPrivateKey makes sure the node always runs with the same operator private key: it
// saves the key's check value on first run and errors if the key doesn't match it later.
//
// Older versions store a private key hash instead, which this version replaces with the check value:
// once the key is verified that hash is dropped. The check value is saved first, so an interrupted
// drop never skips the key check. Older versions still start on the database, as on a first run: they
// accept any key and store its hash again, for the next start of this version to drop.
func ensureOperatorPrivateKey(
	logger *zap.Logger,
	nodeStorage operatorstorage.Storage,
	operatorPrivKey keys.OperatorPrivateKey,
	operatorPrivKeyBase64 string,
) error {
	operatorPubKeyBase64, err := operatorPrivKey.Public().Base64()
	if err != nil {
		return fmt.Errorf("could not get operator public key base64: %w", err)
	}
	checkValue := operatorKeyCheckValue(operatorPubKeyBase64)

	storedCheckValue, checkValueFound, err := nodeStorage.GetOperatorKeyCheckValue()
	if err != nil {
		return fmt.Errorf("could not get operator key check value: %w", err)
	}
	privKeyHash, hashFound, err := nodeStorage.GetPrivateKeyHash()
	if err != nil {
		return fmt.Errorf("could not get hashed private key: %w", err)
	}

	switch {
	case checkValueFound:
		if !bytes.Equal(storedCheckValue, checkValue) {
			return errOperatorKeyMismatch
		}
	case hashFound:
		// StorageHash or, in databases from before it, the hash of the configured key text.
		cliPrivKeyDecoded, err := base64.StdEncoding.DecodeString(operatorPrivKeyBase64)
		if err != nil {
			return fmt.Errorf("could not decode private key: %w", err)
		}
		if !bytes.Equal(privKeyHash, operatorPrivKey.StorageHash()) &&
			!bytes.Equal(privKeyHash, rsaencryption.HashKeyBytes(cliPrivKeyDecoded)) {
			return errOperatorKeyMismatch
		}
	}

	if !checkValueFound {
		if err := nodeStorage.SaveOperatorKeyCheckValue(checkValue); err != nil {
			return fmt.Errorf("could not save operator key check value: %w", err)
		}
	}

	if hashFound {
		if err := nodeStorage.DropPrivateKeyHash(); err != nil {
			return fmt.Errorf("could not drop hashed private key: %w", err)
		}
		logger.Info("dropped the stored private key hash")
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
	privKey       keys.OperatorPrivateKey
	privKeyBase64 string
	ssvSigner     *ssvsigner.Client
	pubKeyBase64  string
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

		return operatorIdentity{ssvSigner: ssvSignerClient, pubKeyBase64: operatorPubKeyBase64}, nil
	}

	var operatorPrivKey keys.OperatorPrivateKey
	var operatorPrivKeyBase64 string
	if res.usingKeystore {
		logger.Info("getting operator private key from keystore")

		privKey, decryptedKeystore, err := privateKeyFromKeystore(cfg.KeyStore.PrivateKeyFile, cfg.KeyStore.PasswordFile)
		if err != nil {
			return operatorIdentity{}, fmt.Errorf("could not extract private key from keystore: %w", err)
		}

		operatorPrivKey = privKey
		operatorPrivKeyBase64 = base64.StdEncoding.EncodeToString(decryptedKeystore)
	} else if res.usingPrivKey {
		logger.Info("getting operator private key from args")

		privKey, err := keys.PrivateKeyFromString(cfg.OperatorPrivateKey)
		if err != nil {
			return operatorIdentity{}, fmt.Errorf("could not decode operator private key: %w", err)
		}

		operatorPrivKey = privKey
		operatorPrivKeyBase64 = cfg.OperatorPrivateKey
	}

	operatorPubKeyBase64, err := operatorPrivKey.Public().Base64()
	if err != nil {
		return operatorIdentity{}, fmt.Errorf("could not get operator public key base64: %w", err)
	}

	return operatorIdentity{privKey: operatorPrivKey, privKeyBase64: operatorPrivKeyBase64, pubKeyBase64: operatorPubKeyBase64}, nil
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
