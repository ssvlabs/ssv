package goclient

import (
	"fmt"
	"time"

	"github.com/ssvlabs/ssv/networkconfig"
)

// Various Client timeouts are defined below.
const (
	// defaultCommonTimeout is the default timeout for dialing, and most client-requests.
	defaultCommonTimeout = time.Second * 5
	// defaultLongTimeout is the default timeout for certain specific operations the client performs.
	defaultLongTimeout = time.Second * 60
)

// BlockFetchPath identifies which block-header fetch strategy the SSV node is using.
// Determined at startup from operator-provided config; see DetermineBlockFetchPath.
//
// Documented end-to-end in docs/MEV_CONSIDERATIONS.md.
type BlockFetchPath int

const (
	// BlockFetchPathSafe is the default. Multi-BN parallel fetch with early-exit on
	// first blinded response; fallback at slot-relative ProposalSoftDeadline (default 1000ms).
	BlockFetchPathSafe BlockFetchPath = iota
	// BlockFetchPathLegacy preserves the original ProposerDelay / ProposalSoftTimeout
	// behavior bit-for-bit; selected when an operator has set either of those legacy knobs.
	BlockFetchPathLegacy
	// BlockFetchPathMEVOptimized is opt-in. Multi-BN parallel fetch without early-exit,
	// returns the best-scored response collected by ProposalSoftDeadline. Selected when an
	// operator sets ProposalSoftDeadline explicitly.
	BlockFetchPathMEVOptimized
)

// String returns a human-readable label for logging.
func (p BlockFetchPath) String() string {
	switch p {
	case BlockFetchPathSafe:
		return "safe"
	case BlockFetchPathLegacy:
		return "legacy"
	case BlockFetchPathMEVOptimized:
		return "mev-optimized"
	default:
		return fmt.Sprintf("unknown(%d)", int(p))
	}
}

// ProposalSoftDeadline bounds and defaults. Values are slot-relative (measured from slot start).
const (
	// DefaultProposalSoftDeadline is the default deadline for the safe path. Picked so
	// the worst-case 2-round QBFT scenario still fits within the 4000ms slot deadline:
	//   1000ms (deadline) + 2500ms (QBFT worst-case 2-round) + 150ms (signing) + 200ms (submission) = 3850ms
	DefaultProposalSoftDeadline = 1000 * time.Millisecond

	// MinProposalSoftDeadline is the lower bound for operator-set ProposalSoftDeadline values.
	// Matches DefaultProposalSoftDeadline — going lower defeats the purpose of opting into the
	// MEV-optimized path (BNs won't have responded yet).
	MinProposalSoftDeadline = DefaultProposalSoftDeadline

	// MaxProposalSoftDeadline is the hard upper bound for operator-set ProposalSoftDeadline
	// values. Intentionally loose — past the SafeMax warning threshold, the operator has
	// already opted into "round 1 must succeed". This cap exists to accommodate exceptionally
	// performant clusters that can complete the entire post-header pipeline (QBFT round 1 +
	// signing + submission) in well under 350ms and want to capture as much of the slot's
	// bid growth as possible. Operators in this regime should baseline their own latencies
	// (see docs/MEV_CONSIDERATIONS.md "Tuning guidance") before going anywhere near the cap.
	MaxProposalSoftDeadline = 3600 * time.Millisecond

	// SafeMaxProposalSoftDeadline is the threshold above which the worst-case 2-round QBFT
	// scenario no longer fits within the slot deadline (round 1 must succeed for the slot).
	// Derived from the typical values in docs/MEV_CONSIDERATIONS.md:
	//   deadline + 50ms (BN→SSV transport) + 2500ms (QBFT worst-case 2-round) +
	//   150ms (PostConsensusSigning) + 200ms (BlockSubmission) <= 4000ms
	//   => deadline <= 1100ms
	// Values above this trigger a startup warning but are still permitted (the operator
	// is explicitly accepting "round 1 must succeed" — Example B is such a setup).
	SafeMaxProposalSoftDeadline = 1100 * time.Millisecond
)

// Legacy-path constants — preserved for backward-compat.
const (
	defaultProposalSoftTimeout = 1800 * time.Millisecond
	minProposalSoftTimeout     = 500 * time.Millisecond
)

// Options defines beacon client options
type Options struct {
	BeaconConfig                *networkconfig.Beacon
	BeaconNodeAddr              string `yaml:"BeaconNodeAddr" env:"BEACON_NODE_ADDR" env-required:"true" env-description:"Beacon node URL(s). Multiple nodes are supported via semicolon-separated URLs (e.g. 'http://localhost:5052;http://localhost:5053')"`
	SyncDistanceTolerance       uint64 `yaml:"SyncDistanceTolerance" env:"BEACON_SYNC_DISTANCE_TOLERANCE" env-default:"4" env-description:"Maximum number of slots behind head considered in-sync"`
	WithWeightedAttestationData bool   `yaml:"WithWeightedAttestationData" env:"WITH_WEIGHTED_ATTESTATION_DATA" env-default:"false" env-description:"Enable attestation data scoring across multiple beacon nodes"`
	WithParallelSubmissions     bool   `yaml:"WithParallelSubmissions" env:"WITH_PARALLEL_SUBMISSIONS" env-default:"false" env-description:"Enables parallel Attestation and Sync Committee submissions to all Beacon nodes (as opposed to submitting to a single Beacon node via multiclient instance)"`

	CommonTimeout time.Duration `yaml:"CommonTimeout" env:"WITH_COMMON_TIMEOUT" env-description:"Specifies the common timeout for network operations"`
	LongTimeout   time.Duration `yaml:"LongTimeout" env:"WITH_LONG_TIMEOUT" env-description:"Specifies the long timeout for network operations"`

	// ProposalSoftTimeout is the legacy collection-period timeout in multi-BN parallel
	// fetch. Setting this (or ProposerDelay) selects BlockFetchPathLegacy. New operators
	// should prefer ProposalSoftDeadline. See docs/MEV_CONSIDERATIONS.md.
	ProposalSoftTimeout time.Duration `yaml:"ProposalSoftTimeout" env:"WITH_PROPOSAL_SOFT_TIMEOUT" env-description:"Legacy MEV configuration. Specifies the beacon proposal collection soft timeout (collection period for comparing proposals from multiple beacon nodes to select the most profitable one). Setting this opts the SSV node into the legacy block-fetch path; the recommended approach is to leave this unset and use ProposalSoftDeadline instead. See https://github.com/ssvlabs/ssv/blob/main/docs/MEV_CONSIDERATIONS.md for details."`

	// ProposalSoftDeadline is the slot-relative deadline (in ms-into-slot) for the
	// multi-BN proposal-collection window used by the safe and MEV-optimized paths.
	//   - Unset (zero) -> safe path, default deadline 1000ms.
	//   - Set explicitly -> MEV-optimized path, value must be in [1000ms, 3600ms].
	// Cannot be combined with ProposerDelay or ProposalSoftTimeout (which select the
	// legacy path).
	ProposalSoftDeadline time.Duration `yaml:"ProposalSoftDeadline" env:"WITH_PROPOSAL_SOFT_DEADLINE" env-description:"Slot-relative deadline (ms into slot) for the multi-BN proposal-collection window. Leave unset for the default safe path; set explicitly to opt into the MEV-optimized path (value must be in [1000ms, 3600ms]). Cannot be combined with ProposerDelay or ProposalSoftTimeout. See https://github.com/ssvlabs/ssv/blob/main/docs/MEV_CONSIDERATIONS.md for details."`

	// BlockFetchPath is set by NewOptions from the determined path; not directly
	// configured by the operator. Consumed by GoClient at runtime to dispatch block
	// fetching to the correct strategy.
	BlockFetchPath BlockFetchPath `yaml:"-"`
}

// DetermineBlockFetchPath returns the block-fetch path selected by the operator's config.
//
// Must be called with raw operator-provided values (before NewOptions applies any defaults)
// so that the "operator explicitly set" vs "defaulted" distinction is preserved.
//
// Returns an error when:
//   - any of the MEV-related duration knobs is negative; or
//   - the config combines path-0 (legacy) knobs with the path-2 (MEV-optimized)
//     ProposalSoftDeadline — operators must pick one.
func DetermineBlockFetchPath(base Options, proposerDelay time.Duration) (BlockFetchPath, error) {
	// Negative values are nonsensical for any of these and would silently be
	// treated as "unset" by the `> 0` checks below — reject them upfront so the
	// operator gets a clear startup error instead of a confusing late-firing
	// soft-deadline or skipped path-0 selection.
	if proposerDelay < 0 {
		return 0, fmt.Errorf("ProposerDelay must be non-negative, got %v", proposerDelay)
	}
	if base.ProposalSoftTimeout < 0 {
		return 0, fmt.Errorf("ProposalSoftTimeout must be non-negative, got %v", base.ProposalSoftTimeout)
	}
	if base.ProposalSoftDeadline < 0 {
		return 0, fmt.Errorf("ProposalSoftDeadline must be non-negative, got %v", base.ProposalSoftDeadline)
	}

	legacySet := proposerDelay > 0 || base.ProposalSoftTimeout > 0
	deadlineSet := base.ProposalSoftDeadline > 0

	if legacySet && deadlineSet {
		return 0, fmt.Errorf("ProposalSoftDeadline conflicts with legacy ProposerDelay/ProposalSoftTimeout config — remove one. See docs/MEV_CONSIDERATIONS.md for path selection guidance")
	}

	switch {
	case legacySet:
		return BlockFetchPathLegacy, nil
	case deadlineSet:
		return BlockFetchPathMEVOptimized, nil
	default:
		return BlockFetchPathSafe, nil
	}
}

// ValidateProposalSoftDeadline ensures the value is within the acceptable range for
// the MEV-optimized fetch path. The caller is responsible for emitting an additional
// log-warning when the value exceeds SafeMaxProposalSoftDeadline.
func ValidateProposalSoftDeadline(d time.Duration) error {
	if d < MinProposalSoftDeadline || d > MaxProposalSoftDeadline {
		return fmt.Errorf("ProposalSoftDeadline value %dms is out of range [%dms, %dms]",
			d.Milliseconds(),
			MinProposalSoftDeadline.Milliseconds(),
			MaxProposalSoftDeadline.Milliseconds())
	}
	return nil
}

// NewOptions applies path-specific defaults to base options and returns the result.
//
// path is the value returned by DetermineBlockFetchPath. proposerDelay is only consumed
// when path == BlockFetchPathLegacy. The selected path is stashed in the returned
// Options.BlockFetchPath for consumption by GoClient at runtime.
func NewOptions(base Options, proposerDelay time.Duration, path BlockFetchPath) (Options, error) {
	options := base
	options.BlockFetchPath = path

	if options.CommonTimeout == 0 {
		options.CommonTimeout = defaultCommonTimeout
	}
	if options.LongTimeout == 0 {
		options.LongTimeout = defaultLongTimeout
	}

	switch path {
	case BlockFetchPathLegacy:
		// Legacy path: preserve the original ProposalSoftTimeout defaulting (1800ms,
		// reduced by ProposerDelay, floored at 500ms). Behavior bit-for-bit unchanged
		// from before the path split was introduced.
		if options.ProposalSoftTimeout == 0 {
			options.ProposalSoftTimeout = defaultProposalSoftTimeout
			// Reduce by proposer delay to maintain consistent duty-execution timelines
			// for different operators in the cluster, ensuring QBFT consensus starts at
			// roughly the same time regardless of proposer-delay configuration.
			if proposerDelay > 0 {
				options.ProposalSoftTimeout -= proposerDelay
			}
		}
		if options.ProposalSoftTimeout < minProposalSoftTimeout {
			options.ProposalSoftTimeout = minProposalSoftTimeout
		}

	case BlockFetchPathSafe:
		// Safe path: slot-relative deadline, default 1000ms.
		if options.ProposalSoftDeadline == 0 {
			options.ProposalSoftDeadline = DefaultProposalSoftDeadline
		}

	case BlockFetchPathMEVOptimized:
		// MEV-optimized path: ProposalSoftDeadline must be set by the operator and
		// validated upstream (ValidateProposalSoftDeadline). No defaults to apply.
	}

	// Note: There is no hard timeout for proposals. The parent context from the
	// duty runner (bounded by slot timing) serves as the ultimate deadline.
	// This ensures we never give up early on getting a block proposal.

	return options, nil
}
