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
	"github.com/ssvlabs/ssv/exporter"
	p2pv1 "github.com/ssvlabs/ssv/network/p2p"
	"github.com/ssvlabs/ssv/operator"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
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
	globalcfg.Global             `yaml:"global"`
	DBOptions                    basedb.Options          `yaml:"db"`
	SSVOptions                   operator.Options        `yaml:"ssv"`
	ExporterOptions              exporter.Options        `yaml:"exporter"`
	ExecutionClient              executionclient.Options `yaml:"eth1"` // TODO: execution_client in yaml
	ConsensusClient              goclient.Options        `yaml:"eth2"` // TODO: consensus_client in yaml
	P2pNetworkConfig             p2pv1.Config            `yaml:"p2p"`
	KeyStore                     KeyStore                `yaml:"KeyStore"`
	SSVSigner                    SSVSignerConfig         `yaml:"SSVSigner" env-prefix:"SSV_SIGNER_"`
	Graffiti                     string                  `yaml:"Graffiti" env:"GRAFFITI" env-description:"Custom graffiti for block proposals" env-default:"ssv.network"`
	ProposerDelay                time.Duration           `yaml:"ProposerDelay" env:"PROPOSER_DELAY" env-description:"Duration to wait out before requesting Ethereum block to propose if this Operator is proposer-duty Leader (eg. 300ms). See https://github.com/ssvlabs/ssv/blob/main/docs/MEV_CONSIDERATIONS.md#appendix-a--legacy-proposerdelay-approach for detailed instructions on how to use it."`
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

// Block-fetch tuning bounds (operator-policy thresholds; goclient consumes the resolved
// values, not these). See docs/MEV_CONSIDERATIONS.md for the derivations.
const (
	// maxSafeProposerDelay is the largest ProposerDelay considered safe. Above this, the
	// worst-case 2-round QBFT scenario risks missing the slot, so the operator must explicitly
	// acknowledge the risk via AllowDangerousProposerDelay.
	maxSafeProposerDelay = 1000 * time.Millisecond

	// ProposalSoftDeadline bounds (slot-relative), used by the MEV-optimized path.
	// [minProposalSoftDeadline, maxProposalSoftDeadline] is the hard accepted range.
	// safeMaxProposalSoftDeadline is the largest value considered safe: above it the worst-case
	// 2-round QBFT fallback may not fit within the slot, so the operator must explicitly
	// acknowledge the risk via AllowDangerousProposalSoftDeadline (mirrors maxSafeProposerDelay /
	// AllowDangerousProposerDelay).
	safeMaxProposalSoftDeadline = 1450 * time.Millisecond
	minProposalSoftDeadline     = 1000 * time.Millisecond
	maxProposalSoftDeadline     = 3600 * time.Millisecond

	// Legacy-path soft-timeout defaulting (1800ms, reduced by ProposerDelay, floored at 500ms).
	defaultProposalSoftTimeout = 1800 * time.Millisecond
	minProposalSoftTimeout     = 500 * time.Millisecond
)

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

// resolveAndValidate resolves + validates the operator configuration: it mutates
// c.ConsensusClient with the resolved block-fetch values (consumed by goclient), emits advisory
// warnings/info (via logger), and returns the derived state (operating mode + signing flags). A
// returned error is fatal — the caller logs it once. logger is used only for warnings/info,
// never for fatal conditions.
func (c *config) resolveAndValidate(logger *zap.Logger) (resolved, error) {
	// Resolve signing first so a doubly-misconfigured node surfaces the signing error before the
	// block-fetch one (matches the pre-MEV ordering).
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

	// Block-fetch: select the path, validate + resolve its knobs onto c.ConsensusClient.
	if err := c.resolveBlockFetch(logger); err != nil {
		return resolved{}, err
	}

	// Resolve the operating mode last so a doubly-misconfigured node still surfaces the signing
	// or block-fetch error first.
	m, err := resolveMode(c.ExporterOptions)
	if err != nil {
		return resolved{}, err
	}
	res.mode = m

	return res, nil
}

// resolveBlockFetch determines the block-fetch path from the operator's config, validates the
// path-specific knobs, resolves their defaults onto c.ConsensusClient (consumed by goclient at
// runtime), and emits advisory warnings. A returned error is fatal.
//
// Must run exactly once (resolveAndValidate, the sole caller, runs it at startup): the legacy
// path resolves ProposalSoftTimeout in place (1800ms reduced by ProposerDelay, floored), so a
// second pass would reduce it twice.
func (c *config) resolveBlockFetch(logger *zap.Logger) error {
	// Raw operator inputs, snapshotted before any resolution writes below — so path selection and
	// defaulting never observe a value that this function itself produced.
	var (
		proposerDelay   = c.ProposerDelay
		rawSoftTimeout  = c.ConsensusClient.ProposalSoftTimeout
		rawSoftDeadline = c.ConsensusClient.ProposalSoftDeadline
	)

	path, err := determineBlockFetchPath(rawSoftTimeout, rawSoftDeadline, proposerDelay)
	if err != nil {
		return err
	}

	switch path {
	case blockFetchPathLegacy:
		if err := validateProposerDelay(proposerDelay, c.AllowDangerousProposerDelay); err != nil {
			return err
		}
		// Default the legacy soft timeout: 1800ms reduced by ProposerDelay, floored at 500ms.
		softTimeout := rawSoftTimeout
		if softTimeout == 0 {
			softTimeout = defaultProposalSoftTimeout
			if proposerDelay > 0 {
				softTimeout -= proposerDelay
			}
		}
		if softTimeout < minProposalSoftTimeout {
			softTimeout = minProposalSoftTimeout
		}
		// goclient keys the legacy (relative-timeout) path off ProposalSoftTimeout > 0.
		c.ConsensusClient.ProposalSoftTimeout = softTimeout
	case blockFetchPathMEVOptimized:
		// The operator-set ProposalSoftDeadline is validated and passed through unchanged; goclient
		// keys the MEV-optimized (slot-relative) path off it. Applies to single- and multi-BN
		// setups alike. See docs/MEV_CONSIDERATIONS.md.
		if err := validateProposalSoftDeadline(rawSoftDeadline, c.ConsensusClient.AllowDangerousProposalSoftDeadline); err != nil {
			return err
		}
	}

	logger.Info("block-fetch path selected", zap.String("path", path.String()))

	// Advisory warnings — emitted after validation, so they never precede a validation error.
	switch path {
	case blockFetchPathLegacy:
		if proposerDelay > maxSafeProposerDelay {
			// Reachable only after validateProposerDelay passed (AllowDangerousProposerDelay set).
			logger.Warn("Using dangerous ProposerDelay value that may cause missed block proposals",
				zap.Int64("proposer_delay_ms", proposerDelay.Milliseconds()),
				zap.Int64("max_safe_proposer_delay_ms", maxSafeProposerDelay.Milliseconds()))
		}
	case blockFetchPathMEVOptimized:
		if rawSoftDeadline > safeMaxProposalSoftDeadline {
			// Reachable only after validateProposalSoftDeadline passed (AllowDangerousProposalSoftDeadline set).
			logger.Warn(
				"ProposalSoftDeadline exceeds the safe-max threshold: "+
					"round-2 QBFT fallback may not fit within the slot deadline "+
					"for clusters with typical latencies, "+
					"so the slot may be missed when round 1 fails "+
					"(this is an explicit 'round 1 must succeed' configuration).",
				zap.Int64("proposal_soft_deadline_ms", rawSoftDeadline.Milliseconds()),
				zap.Int64("safe_max_ms", safeMaxProposalSoftDeadline.Milliseconds()))
		}
	}

	return nil
}

// blockFetchPath is the operator-facing block-fetch strategy selected at startup from config.
// It is policy vocabulary owned by the config layer; resolveBlockFetch resolves it into the timing
// field goclient keys off (ProposalSoftDeadline for MEV-optimized, ProposalSoftTimeout for legacy).
// Documented end-to-end in docs/MEV_CONSIDERATIONS.md.
type blockFetchPath int

const (
	// blockFetchPathLegacy is the default: relative-timeout collection that early-exits on the
	// first blinded response. Selected when neither ProposalSoftDeadline nor the legacy knobs are
	// set, or when an operator sets ProposerDelay / ProposalSoftTimeout explicitly.
	blockFetchPathLegacy blockFetchPath = iota
	// blockFetchPathMEVOptimized is opt-in: slot-relative collection (no early-exit) that returns
	// the best-scored response collected by ProposalSoftDeadline and starts QBFT at that
	// slot-relative deadline (single- and multi-BN setups alike). Selected when an operator sets
	// ProposalSoftDeadline explicitly.
	blockFetchPathMEVOptimized
)

// String returns a human-readable label for logging.
func (p blockFetchPath) String() string {
	switch p {
	case blockFetchPathLegacy:
		return "legacy"
	case blockFetchPathMEVOptimized:
		return "mev-optimized"
	default:
		return fmt.Sprintf("unknown(%d)", int(p))
	}
}

// determineBlockFetchPath selects the block-fetch path from the operator's raw timing knobs:
// ProposalSoftDeadline set -> MEV-optimized; otherwise (nothing set, or ProposerDelay /
// ProposalSoftTimeout set) -> legacy (the default). Negative durations, and combining the legacy
// knobs with ProposalSoftDeadline, are rejected.
func determineBlockFetchPath(proposalSoftTimeout, proposalSoftDeadline, proposerDelay time.Duration) (blockFetchPath, error) {
	if proposerDelay < 0 {
		return 0, fmt.Errorf("ProposerDelay must be non-negative, got %v", proposerDelay)
	}
	if proposalSoftTimeout < 0 {
		return 0, fmt.Errorf("ProposalSoftTimeout must be non-negative, got %v", proposalSoftTimeout)
	}
	if proposalSoftDeadline < 0 {
		return 0, fmt.Errorf("ProposalSoftDeadline must be non-negative, got %v", proposalSoftDeadline)
	}

	legacySet := proposerDelay > 0 || proposalSoftTimeout > 0
	deadlineSet := proposalSoftDeadline > 0

	if legacySet && deadlineSet {
		return 0, fmt.Errorf("ProposalSoftDeadline conflicts with legacy ProposerDelay/ProposalSoftTimeout config — remove one. See docs/MEV_CONSIDERATIONS.md for path selection guidance")
	}

	if deadlineSet {
		return blockFetchPathMEVOptimized, nil
	}
	// Default (nothing set) and the explicit legacy knobs both resolve to the legacy path.
	return blockFetchPathLegacy, nil
}

// validateProposalSoftDeadline ensures an operator-set ProposalSoftDeadline (MEV-optimized path)
// is within the hard [min, max] range, and rejects a value above safeMaxProposalSoftDeadline
// unless the operator explicitly acknowledges the risk via allowDangerous (mirrors
// validateProposerDelay). The safe-max WARN is emitted separately.
func validateProposalSoftDeadline(d time.Duration, allowDangerous bool) error {
	if d < minProposalSoftDeadline || d > maxProposalSoftDeadline {
		return fmt.Errorf("ProposalSoftDeadline value %dms is out of range [%dms, %dms]",
			d.Milliseconds(),
			minProposalSoftDeadline.Milliseconds(),
			maxProposalSoftDeadline.Milliseconds())
	}
	if d > safeMaxProposalSoftDeadline && !allowDangerous {
		return fmt.Errorf("ProposalSoftDeadline value %dms exceeds maximum safe deadline of %dms. "+
			"This may cause missed block proposals. "+
			"If you understand the risks and want to proceed, set AllowDangerousProposalSoftDeadline to true or use the ALLOW_DANGEROUS_PROPOSAL_SOFT_DEADLINE environment variable",
			d.Milliseconds(), safeMaxProposalSoftDeadline.Milliseconds())
	}
	return nil
}

// validateProposerDelay rejects a ProposerDelay above maxSafeProposerDelay unless the operator
// explicitly acknowledges the risk via allowDangerous.
func validateProposerDelay(proposerDelay time.Duration, allowDangerous bool) error {
	if proposerDelay > maxSafeProposerDelay && !allowDangerous {
		return fmt.Errorf("ProposerDelay value %dms exceeds maximum safe delay of %dms. "+
			"This may cause missed block proposals. "+
			"If you understand the risks and want to proceed, set AllowDangerousProposerDelay to true or use the ALLOW_DANGEROUS_PROPOSER_DELAY environment variable",
			proposerDelay.Milliseconds(), maxSafeProposerDelay.Milliseconds())
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

// startupError wraps an error returned from runNode() with structured log fields, so the single
// top-level fatal preserves the context (e.g. the ssv-signer endpoint). Mirrors signingConfigError,
// for non-validation startup failures.
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

// startupErrorLogFields returns the structured log fields for an error returned by runNode(): the
// error itself, plus any context carried by a fieldedError (signingConfigError / startupError).
func startupErrorLogFields(err error) []zap.Field {
	fields := []zap.Field{zap.Error(err)}
	var fe fieldedError
	if errors.As(err, &fe) {
		fields = append(fields, fe.logFields()...)
	}
	return fields
}
