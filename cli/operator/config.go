package operator

import (
	"fmt"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/beacon/goclient"
	global_config "github.com/ssvlabs/ssv/cli/config"
	"github.com/ssvlabs/ssv/eth/executionclient"
	"github.com/ssvlabs/ssv/exporter"
	p2pv1 "github.com/ssvlabs/ssv/network/p2p"
	"github.com/ssvlabs/ssv/operator"
	"github.com/ssvlabs/ssv/storage/basedb"
)

type KeyStore struct {
	PrivateKeyFile string `yaml:"PrivateKeyFile" env:"PRIVATE_KEY_FILE" env-description:"Path to operator private key file"`
	PasswordFile   string `yaml:"PasswordFile" env:"PASSWORD_FILE" env-description:"Path to password file for private key decryption"`
}

type SSVSignerConfig struct {
	Endpoint             string        `yaml:"Endpoint" env:"ENDPOINT" env-description:"Endpoint of ssv-signer. It must be a correct URL"`
	RequestTimeout       time.Duration `yaml:"RequestTimeout" env:"REQUEST_TIMEOUT" env-description:"Request timeout for ssv-signer" env-default:"10s"`
	KeystoreFile         string        `yaml:"KeystoreFile" env:"KEYSTORE_FILE" env-description:"Path to ssv-signer client keystore file"`
	KeystorePasswordFile string        `yaml:"KeystorePasswordFile" env:"KEYSTORE_PASSWORD_FILE" env-description:"Path to file containing the password for client keystore file"`
	ServerCertFile       string        `yaml:"ServerCertFile" env:"SERVER_CERT_FILE" env-description:"Path to trusted server certificate file for ssv-signer"`
}

type config struct {
	global_config.Global         `yaml:"global"`
	DBOptions                    basedb.Options          `yaml:"db"`
	SSVOptions                   operator.Options        `yaml:"ssv"`
	ExporterOptions              exporter.Options        `yaml:"exporter"`
	ExecutionClient              executionclient.Options `yaml:"eth1"` // TODO: execution_client in yaml
	ConsensusClient              goclient.Options        `yaml:"eth2"` // TODO: consensus_client in yaml
	P2pNetworkConfig             p2pv1.Config            `yaml:"p2p"`
	KeyStore                     KeyStore                `yaml:"KeyStore"`
	SSVSigner                    SSVSignerConfig         `yaml:"SSVSigner" env-prefix:"SSV_SIGNER_"`
	Graffiti                     string                  `yaml:"Graffiti" env:"GRAFFITI" env-description:"Custom graffiti for block proposals" env-default:"ssv.network"`
	ProposerDelay                time.Duration           `yaml:"ProposerDelay" env:"PROPOSER_DELAY" env-description:"Duration to wait out before requesting Ethereum block to propose if this Operator is proposer-duty Leader (eg. 300ms). See https://github.com/ssvlabs/ssv/blob/main/docs/MEV_CONSIDERATIONS.md#getting-started-with-mev-configuration for detailed instructions on how to use it."`
	AllowDangerousProposerDelay  bool                    `yaml:"AllowDangerousProposerDelay" env:"ALLOW_DANGEROUS_PROPOSER_DELAY" env-description:"Allow ProposerDelay values higher than 1s (dangerous, may cause missed block proposals)"`
	OperatorPrivateKey           string                  `yaml:"OperatorPrivateKey" env:"OPERATOR_KEY" env-description:"Operator private key for contract event decryption"`
	MetricsAPIPort               int                     `yaml:"MetricsAPIPort" env:"METRICS_API_PORT" env-description:"Port for metrics API server"`
	EnableTraces                 bool                    `yaml:"EnableTraces" env:"ENABLE_TRACES" env-description:"Enable Open Telemetry traces"`
	EnableProfile                bool                    `yaml:"EnableProfile" env:"ENABLE_PROFILE" env-description:"Enable Go profiling tools"`
	NetworkPrivateKey            string                  `yaml:"NetworkPrivateKey" env:"NETWORK_PRIVATE_KEY" env-description:"Private key for P2P network identity"`
	WsAPIPort                    int                     `yaml:"WebSocketAPIPort" env:"WS_API_PORT" env-description:"Port for WebSocket API server"`
	WithPing                     bool                    `yaml:"WithPing" env:"WITH_PING" env-description:"Enable WebSocket ping messages"`
	SSVAPIAddress                string                  `yaml:"SSVAPIAddress" env:"SSV_API_ADDRESS" env-description:"Listen address for SSV API server. Leave empty to listen on all interfaces; use 127.0.0.1 to keep it local-only"`
	SSVAPIPort                   int                     `yaml:"SSVAPIPort" env:"SSV_API_PORT" env-description:"Port for SSV API server"`
	LocalEventsPath              string                  `yaml:"LocalEventsPath" env:"EVENTS_PATH" env-description:"Path to local events file"`
	EnableDoppelgangerProtection bool                    `yaml:"EnableDoppelgangerProtection" env:"ENABLE_DOPPELGANGER_PROTECTION" env-description:"Enable doppelganger protection for validators"`
}

var cfg config

var globalArgs global_config.Args

func init() {
	global_config.ProcessArgs(&cfg, &globalArgs, StartNodeCmd)
}

// maxSafeProposerDelay is the largest ProposerDelay considered safe. Above this, the
// worst-case 2-round QBFT scenario risks missing the slot, so the operator must explicitly
// acknowledge the risk via AllowDangerousProposerDelay.
const maxSafeProposerDelay = 1000 * time.Millisecond

// resolved carries config-DERIVED state (computed during resolveAndValidate, not directly
// operator-provided) that startup needs but that (a) has no home as a field on config and
// (b) is consumed only within cli/operator. General-purpose bucket: today it holds the
// signing-mode flags; extend it as more such derived state is consolidated here.
type resolved struct {
	usingSSVSigner bool
	usingKeystore  bool
	usingPrivKey   bool
}

// load reads the operator config (and optional share config) from the paths in globalArgs.
// Called before the zap logger exists, so the caller handles failures via the std logger.
func (c *config) load() error {
	if globalArgs.ConfigPath != "" {
		if err := cleanenv.ReadConfig(globalArgs.ConfigPath, c); err != nil {
			return fmt.Errorf("could not read config needed for logger initialization: %w", err)
		}
	}
	if globalArgs.ShareConfigPath != "" {
		if err := cleanenv.ReadConfig(globalArgs.ShareConfigPath, c); err != nil {
			return fmt.Errorf("could not read share config needed for logger initialization: %w", err)
		}
	}
	return nil
}

// resolveAndValidate validates the operator configuration, emits advisory warnings (via
// logger), and returns derived signing state. A returned error is fatal — the caller logs it
// once. logger is used only for warnings, never for fatal conditions.
func (c *config) resolveAndValidate(logger *zap.Logger) (resolved, error) {
	// Signing first (matches the pre-refactor order: signing config was asserted before the
	// ProposerDelay check), so the same error surfaces first for a doubly-misconfigured node.
	var res resolved
	if c.ExporterOptions.Enabled {
		c.warnExporterSigning(logger)
	} else {
		var err error
		if res, err = c.resolveSigning(); err != nil {
			// Surface the configured signing sources alongside the error — the pre-refactor
			// assertSigningConfig attached these as structured log fields before its Fatal;
			// keeping them preserves misconfiguration-triage context (the private key itself
			// is never logged, only whether it is set).
			return resolved{}, fmt.Errorf("%w "+
				"[SSVSigner.Endpoint=%q KeyStore.PrivateKeyFile=%q KeyStore.PasswordFile=%q OperatorPrivateKey set=%t]",
				err, c.SSVSigner.Endpoint, c.KeyStore.PrivateKeyFile, c.KeyStore.PasswordFile, c.OperatorPrivateKey != "")
		}
	}

	// ProposerDelay validation runs in both exporter and non-exporter modes.
	if err := validateProposerDelay(c.ProposerDelay, c.AllowDangerousProposerDelay); err != nil {
		return resolved{}, err
	}
	if c.ProposerDelay > maxSafeProposerDelay {
		// Reachable only after validateProposerDelay passed, i.e. AllowDangerousProposerDelay is set.
		logger.Warn("Using dangerous ProposerDelay value that may cause missed block proposals",
			zap.Duration("proposer_delay", c.ProposerDelay),
			zap.Duration("max_safe_proposer_delay", maxSafeProposerDelay))
	}

	return res, nil
}

// validateProposerDelay rejects a ProposerDelay above maxSafeProposerDelay unless the operator
// explicitly acknowledges the risk via allowDangerous.
func validateProposerDelay(proposerDelay time.Duration, allowDangerous bool) error {
	if proposerDelay > maxSafeProposerDelay && !allowDangerous {
		return fmt.Errorf("ProposerDelay value %v exceeds maximum safe delay of %v. "+
			"This may cause missed block proposals. "+
			"If you understand the risks and want to proceed, set AllowDangerousProposerDelay to true or use the ALLOW_DANGEROUS_PROPOSER_DELAY environment variable",
			proposerDelay, maxSafeProposerDelay)
	}
	return nil
}

// resolveSigning determines which signing method the operator configured and validates that
// mutually-exclusive methods aren't combined. Returns the resolved signing flags.
func (c *config) resolveSigning() (resolved, error) {
	var res resolved
	if c.SSVSigner.Endpoint != "" {
		res.usingSSVSigner = true
	}
	if c.KeyStore.PrivateKeyFile != "" || c.KeyStore.PasswordFile != "" {
		if c.KeyStore.PrivateKeyFile == "" || c.KeyStore.PasswordFile == "" {
			return resolved{}, fmt.Errorf("both keystore and password files must be provided if using keystore")
		}
		res.usingKeystore = true
	}
	if c.OperatorPrivateKey != "" {
		res.usingPrivKey = true
	}

	if res.usingSSVSigner && (res.usingKeystore || res.usingPrivKey) {
		return resolved{}, fmt.Errorf("cannot enable both remote signing (SSVSigner.Endpoint) and local signing (PrivateKeyFile/OperatorPrivateKey)")
	} else if res.usingKeystore && res.usingPrivKey {
		return resolved{}, fmt.Errorf("cannot enable both OperatorPrivateKey and PrivateKeyFile")
	}

	return res, nil
}

// warnExporterSigning warns when signing configuration is provided in exporter mode (where it
// is ignored).
func (c *config) warnExporterSigning(logger *zap.Logger) {
	if c.SSVSigner.Endpoint == "" &&
		c.SSVSigner.KeystoreFile == "" &&
		c.SSVSigner.KeystorePasswordFile == "" &&
		c.SSVSigner.ServerCertFile == "" &&
		c.KeyStore.PrivateKeyFile == "" &&
		c.KeyStore.PasswordFile == "" &&
		c.OperatorPrivateKey == "" {
		return
	}

	logger.Warn(
		"exporter mode ignores operator signing configuration",
		zap.String("ssv_signer_endpoint", c.SSVSigner.Endpoint),
		zap.String("ssv_signer_keystore_file", c.SSVSigner.KeystoreFile),
		zap.String("ssv_signer_keystore_password_file", c.SSVSigner.KeystorePasswordFile),
		zap.String("ssv_signer_server_cert_file", c.SSVSigner.ServerCertFile),
		zap.String("operator_private_key_file", c.KeyStore.PrivateKeyFile),
		zap.String("operator_private_key_password_file", c.KeyStore.PasswordFile),
		zap.Int("operator_private_key_len", len(c.OperatorPrivateKey)), // not exposing the private key
	)
}
