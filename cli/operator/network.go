package operator

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"go.uber.org/zap"

	ssv_identity "github.com/ssvlabs/ssv/identity"
	"github.com/ssvlabs/ssv/network"
	p2pv1 "github.com/ssvlabs/ssv/network/p2p"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/ssvsigner"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/storage/basedb"
)

func setupSSVNetwork(logger *zap.Logger, cfg *config) (*networkconfig.SSV, error) {
	var ssvConfig *networkconfig.SSV

	if cfg.SSVOptions.CustomNetwork != nil {
		ssvConfig = cfg.SSVOptions.CustomNetwork
		logger.Info("using custom network config")
	} else if cfg.SSVOptions.NetworkName != "" {
		snc, err := networkconfig.SSVConfigByName(cfg.SSVOptions.NetworkName)
		if err != nil {
			return nil, err
		}
		ssvConfig = snc
		logger.Info("found network config by name",
			zap.String("name", cfg.SSVOptions.NetworkName),
		)
	} else {
		return nil, fmt.Errorf("no ssv network configured: set Network (e.g. \"mainnet\") or CustomNetwork")
	}

	if cfg.SSVOptions.CustomDomainType != "" {
		if !strings.HasPrefix(cfg.SSVOptions.CustomDomainType, "0x") {
			return nil, fmt.Errorf("custom domain type must be a hex string")
		}
		domainBytes, err := hex.DecodeString(cfg.SSVOptions.CustomDomainType[2:])
		if err != nil {
			return nil, fmt.Errorf("failed to decode custom domain type: %w", err)
		}
		if len(domainBytes) != 4 {
			return nil, fmt.Errorf("custom domain type must be 4 bytes")
		}

		// Shallow-copy here before updating DomainType below — otherwise the update mutates the underlying global
		// value ssvConfig is pointing to.
		cfgCopy := *ssvConfig
		ssvConfig = &cfgCopy
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
		return fmt.Errorf("probe remote network key protector mismatch")
	}
	return nil
}
