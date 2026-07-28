package operator

import (
	"errors"
	"fmt"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/beacon/goclient"
	globalcfg "github.com/ssvlabs/ssv/cli/config"
	"github.com/ssvlabs/ssv/eth/executionclient"
	exporterconfig "github.com/ssvlabs/ssv/exporter/config"
	p2pv1 "github.com/ssvlabs/ssv/network/p2p"
	"github.com/ssvlabs/ssv/operator"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
	"github.com/ssvlabs/ssv/storage/basedb"
)

type KeyStore struct {
	PrivateKeyFile string `yaml:"PrivateKeyFile" env:"PRIVATE_KEY_FILE" env-description:"Path to operator private key file"`
	PasswordFile   string `yaml:"PasswordFile" env:"PASSWORD_FILE" env-description:"Path to password file for private key decryption"`
}

type SSVSignerConfig struct {
	Endpoint             string        `yaml:"Endpoint" env:"ENDPOINT" env-description:"Endpoint of ssv-signer. It must be a correct URL"`
	RequestTimeout       time.Duration `yaml:"RequestTimeout" env:"REQUEST_TIMEOUT" env-description:"Request timeout for ssv-signer"`
	KeystoreFile         string        `yaml:"KeystoreFile" env:"KEYSTORE_FILE" env-description:"Path to ssv-signer client keystore file"`
	KeystorePasswordFile string        `yaml:"KeystorePasswordFile" env:"KEYSTORE_PASSWORD_FILE" env-description:"Path to file containing the password for client keystore file"`
	ServerCertFile       string        `yaml:"ServerCertFile" env:"SERVER_CERT_FILE" env-description:"Path to trusted server certificate file for ssv-signer"`
}

func (c *SSVSignerConfig) ApplyDefaults() {
	c.RequestTimeout = 10 * time.Second
}

type config struct {
	globalcfg.Global             `yaml:"global"`
	DBOptions                    basedb.Options          `yaml:"db"`
	SSVOptions                   operator.Options        `yaml:"ssv"`
	ExporterOptions              exporterconfig.Options  `yaml:"exporter"`
	ExecutionClient              executionclient.Options `yaml:"eth1"` // TODO: execution_client in yaml
	ConsensusClient              goclient.Options        `yaml:"eth2"` // TODO: consensus_client in yaml
	P2pNetworkConfig             p2pv1.Config            `yaml:"p2p"`
	KeyStore                     KeyStore                `yaml:"KeyStore"`
	SSVSigner                    SSVSignerConfig         `yaml:"SSVSigner" env-prefix:"SSV_SIGNER_"`
	Graffiti                     string                  `yaml:"Graffiti" env:"GRAFFITI" env-description:"Custom graffiti for block proposals"`
	ProposerDelay                time.Duration           `yaml:"ProposerDelay" env:"PROPOSER_DELAY" env-description:"Duration to wait out before requesting Ethereum block to propose if this Operator is proposer-duty Leader (eg. 300ms). See https://github.com/ssvlabs/ssv/blob/main/docs/MEV_CONSIDERATIONS.md#getting-started-with-mev-configuration for detailed instructions on how to use it."`
	AllowDangerousProposerDelay  bool                    `yaml:"AllowDangerousProposerDelay" env:"ALLOW_DANGEROUS_PROPOSER_DELAY" env-description:"Allow ProposerDelay values higher than 1s (dangerous, may cause missed block proposals)"`
	ProposerDelayEPBS            time.Duration           `yaml:"ProposerDelayEPBS" env:"PROPOSER_DELAY_EPBS" env-description:"Post-ePBS (Gloas) counterpart of ProposerDelay, applied from the Gloas fork on (ProposerDelay applies before it). Hard-capped at 1s with no dangerous override. Default 0 (opt-in)."`
	Builders                     []gloas.BuilderEntry    `yaml:"Builders" env-description:"Gloas (ePBS) direct-builder connections (opt-in overlay, YAML only). Entries must be configured identically across all operators of every shared committee; see docs/EXTERNAL_BUILDERS.md"`
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

// resolved carries config-derived state computed by resolveAndValidate (not operator-provided):
// the operating mode and the signing-method flags.
type resolved struct {
	usingSSVSigner bool
	usingKeystore  bool
	usingPrivKey   bool
	mode           nodeMode
}

// isExporter reports whether the node runs as an exporter (standard or archive) rather than as an
// operator. It is the single mode-axis predicate the startup path dispatches on.
func (r resolved) isExporter() bool {
	return r.mode != modeOperator
}

// ApplyDefaults seeds the operator config defaults in code (see cli/config.Defaulter), composing
// each section's own defaults. The env-required eth1 ETH1Addr / eth2 BeaconNodeAddr are left unset
// so cleanenv still enforces them.
func (c *config) ApplyDefaults() {
	c.Graffiti = "ssv.network"
	c.Global.ApplyDefaults()
	c.DBOptions.ApplyDefaults()
	c.SSVOptions.ApplyDefaults()
	c.ExporterOptions.ApplyDefaults()
	c.ExecutionClient.ApplyDefaults()
	c.ConsensusClient.ApplyDefaults()
	c.P2pNetworkConfig.ApplyDefaults()
	c.SSVSigner.ApplyDefaults()
}

// load reads the operator config (and optional share config) from the given paths. Paths are
// passed in (rather than read from the globalArgs global) so it can be tested in isolation.
// Called before the zap logger exists, so the caller handles failures via the std logger.
func (c *config) load(configPath, shareConfigPath string) error {
	// Seed defaults before reading, so an explicit YAML/env value wins over the default (see
	// cli/config.Defaulter, #2868).
	c.ApplyDefaults()

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

// resolveAndValidate validates the operator configuration, emits advisory warnings, and returns
// the derived state (operating mode + signing flags). A returned error is fatal — the caller logs
// it once. logger is used only for warnings, never for fatal conditions.
func (c *config) resolveAndValidate(logger *zap.Logger) (resolved, error) {
	// Resolve signing before the proposer-delay check so a doubly-misconfigured node surfaces
	// the signing error first.
	var res resolved
	if c.ExporterOptions.Enabled {
		c.warnExporterSigning(logger)
	} else {
		var err error
		if res, err = c.resolveSigning(); err != nil {
			// Carry the configured signing-source context so the caller can attach it as
			// structured fields on the fatal log line (the private key value is never logged).
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

	// ProposerDelayEPBS applies from the Gloas fork on and has no dangerous-override escape hatch:
	// the post-ePBS proposal deadline is tighter, so the cap is enforced unconditionally.
	if c.ProposerDelayEPBS > maxSafeProposerDelay {
		return resolved{}, fmt.Errorf("ProposerDelayEPBS value %v exceeds maximum safe delay of %v (no override is available for the post-ePBS delay)",
			c.ProposerDelayEPBS, maxSafeProposerDelay)
	}

	if err := gloas.ValidateBuilderEntries(c.Builders); err != nil {
		return resolved{}, fmt.Errorf("invalid Builders configuration: %w", err)
	}

	// Resolve the operating mode last so a doubly-misconfigured node still surfaces the signing
	// or proposer-delay error first.
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

// resolveSigning determines which signing method the operator configured and validates that
// exactly one method is set: mutually-exclusive methods aren't combined, and at least one source
// is configured (resolveSigning runs only for operator mode, which must sign). Returns the
// resolved signing flags.
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

	if !res.usingSSVSigner && !res.usingKeystore && !res.usingPrivKey {
		return resolved{}, fmt.Errorf("no operator signing configured: set one of SSVSigner.Endpoint, KeyStore (PrivateKeyFile + PasswordFile), or OperatorPrivateKey")
	}

	return res, nil
}

// resolveMode derives the node's operating mode from the exporter options, rejecting an
// unrecognized EXPORTER_MODE up front (fail-fast). A non-exporter node is always modeOperator,
// regardless of the (then-irrelevant) EXPORTER_MODE.
func resolveMode(opts exporterconfig.Options) (nodeMode, error) {
	if !opts.Enabled {
		return modeOperator, nil
	}
	switch opts.Mode {
	case exporterconfig.ModeStandard:
		return modeExporterStandard, nil
	case exporterconfig.ModeArchive:
		return modeExporterArchive, nil
	default:
		return modeOperator, fmt.Errorf("invalid exporter mode %q (must be %q or %q)", opts.Mode, exporterconfig.ModeStandard, exporterconfig.ModeArchive)
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

// signingLogFields returns the configured signing-source fields for diagnostics. The private
// key value is never included — only its length.
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

func (e signingConfigError) Error() string          { return e.err.Error() }
func (e signingConfigError) Unwrap() error          { return e.err }
func (e signingConfigError) logFields() []zap.Field { return e.fields }

// startupError attaches structured log fields to a startup error, so whoever logs it preserves
// context (e.g. the ssv-signer endpoint) that the message alone can't carry. Mirrors
// signingConfigError, for non-validation startup failures.
type startupError struct {
	err    error
	fields []zap.Field
}

func (e startupError) Error() string          { return e.err.Error() }
func (e startupError) Unwrap() error          { return e.err }
func (e startupError) logFields() []zap.Field { return e.fields }

// fieldedError is implemented by the startup error types that carry structured log fields.
// startupErrorLogFields matches it with errors.As, which returns the outermost such error in
// the chain — so wrapping one field-carrier inside another never double-counts its fields.
type fieldedError interface {
	logFields() []zap.Field
}

// startupErrorLogFields returns the structured log fields for a startup error: the error itself,
// plus any context carried by a fieldedError (signingConfigError / startupError).
func startupErrorLogFields(err error) []zap.Field {
	fields := []zap.Field{zap.Error(err)}
	var fe fieldedError
	if errors.As(err, &fe) {
		fields = append(fields, fe.logFields()...)
	}
	return fields
}
