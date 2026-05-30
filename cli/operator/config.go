package operator

import (
	"errors"
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

// nodeMode is the resolved operating mode of the node, derived once from ExporterOptions by
// resolveAndValidate so startup can dispatch on a typed value instead of re-deriving the mode
// from ExporterOptions.Enabled / .Mode at each site.
type nodeMode int

const (
	modeOperator         nodeMode = iota // not an exporter
	modeExporterStandard                 // exporter, standard tracing
	modeExporterArchive                  // exporter, archive tracing (pre-consensus + consensus)
)

// resolved carries config-DERIVED state (computed during resolveAndValidate, not directly
// operator-provided) that startup needs but that (a) has no home as a field on config and
// (b) is consumed only within cli/operator: the operating mode and the signing-method flags.
type resolved struct {
	usingSSVSigner bool
	usingKeystore  bool
	usingPrivKey   bool
	mode           nodeMode
}

// load reads the operator config (and optional share config) from the given paths. Paths are
// passed in (rather than read from the globalArgs global) so it can be tested in isolation.
// Called before the zap logger exists, so the caller handles failures via the std logger.
func (c *config) load(configPath, shareConfigPath string) error {
	if configPath != "" {
		if err := cleanenv.ReadConfig(configPath, c); err != nil {
			return fmt.Errorf("could not read config needed for logger initialization: %w", err)
		}
	}
	if shareConfigPath != "" {
		if err := cleanenv.ReadConfig(shareConfigPath, c); err != nil {
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
			// Carry the configured signing-source context so the caller can attach it as
			// structured fields on the fatal log line — matching the pre-refactor
			// assertSigningConfig observability (the private key value is never logged).
			return resolved{}, signingConfigError{err: err, fields: c.signingLogFields()}
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

	// Resolve the operating mode last so a doubly-misconfigured node still surfaces the signing
	// or proposer-delay error first (matching pre-refactor precedence, where an invalid
	// EXPORTER_MODE was the latest of these checks).
	m, err := resolveMode(c.ExporterOptions)
	if err != nil {
		return resolved{}, err
	}
	res.mode = m

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

// resolveMode derives the node's operating mode from the exporter options. An unrecognized
// EXPORTER_MODE is rejected here (fail-fast) instead of late in the exporter collector switch.
// A non-exporter node is always modeOperator, regardless of the (then-irrelevant) EXPORTER_MODE.
func resolveMode(opts exporter.Options) (nodeMode, error) {
	if !opts.Enabled {
		return modeOperator, nil
	}
	switch opts.Mode {
	case exporter.ModeStandard:
		return modeExporterStandard, nil
	case exporter.ModeArchive:
		return modeExporterArchive, nil
	default:
		return modeOperator, fmt.Errorf("invalid exporter mode %q (must be %q or %q)", opts.Mode, exporter.ModeStandard, exporter.ModeArchive)
	}
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

// signingLogFields returns the configured signing-source fields for diagnostics, matching the
// pre-refactor assertSigningConfig fields. The private key value is never included — only its
// length.
func (c *config) signingLogFields() []zap.Field {
	return []zap.Field{
		zap.String("ssv_signer_endpoint", c.SSVSigner.Endpoint),
		zap.String("private_key_file", c.KeyStore.PrivateKeyFile),
		zap.String("password_file", c.KeyStore.PasswordFile),
		zap.Int("operator_private_key_len", len(c.OperatorPrivateKey)),
	}
}

// signingConfigError wraps a signing-configuration error with the configured signing-source
// log fields, so the caller can attach them as structured fields on the fatal log line.
type signingConfigError struct {
	err    error
	fields []zap.Field
}

func (e signingConfigError) Error() string { return e.err.Error() }
func (e signingConfigError) Unwrap() error { return e.err }

// configErrorLogFields returns the structured log fields for an error returned by
// resolveAndValidate: the error itself, plus any signing-source context carried by a
// signingConfigError (so the consolidated fatal preserves the pre-refactor structured fields).
func configErrorLogFields(err error) []zap.Field {
	fields := []zap.Field{zap.Error(err)}
	var sce signingConfigError
	if errors.As(err, &sce) {
		fields = append(fields, sce.fields...)
	}
	return fields
}
