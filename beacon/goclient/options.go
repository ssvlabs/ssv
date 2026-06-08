package goclient

import (
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
	// fetch. Setting this (or ProposerDelay) selects the legacy relative-timeout collection.
	// New operators should prefer ProposalSoftDeadline. See docs/MEV_CONSIDERATIONS.md.
	ProposalSoftTimeout time.Duration `yaml:"ProposalSoftTimeout" env:"WITH_PROPOSAL_SOFT_TIMEOUT" env-description:"Legacy MEV configuration. Specifies the beacon proposal collection soft timeout (collection period for comparing proposals from multiple beacon nodes to select the most profitable one). Cannot be set lower than 500ms, to leave the Beacon node enough time to serve the block-fetch request. Setting this opts the SSV node into the legacy block-fetch path; the recommended approach is to leave this unset and use ProposalSoftDeadline instead. See https://github.com/ssvlabs/ssv/blob/main/docs/MEV_CONSIDERATIONS.md for details."`

	// ProposalSoftDeadline is the slot-relative deadline (in ms-into-slot) for the MEV-optimized
	// proposal-collection window.
	//   - Unset (zero) -> legacy (default) relative-timeout path.
	//   - Set explicitly -> MEV-optimized path: collect proposals until this slot-relative
	//     deadline (no early-exit), then start QBFT at it. Applies to single- and multi-BN setups
	//     alike, so all operators in the cluster start QBFT at the same slot-relative time.
	// Cannot be combined with ProposerDelay or ProposalSoftTimeout (which select the legacy path).
	ProposalSoftDeadline time.Duration `yaml:"ProposalSoftDeadline" env:"WITH_PROPOSAL_SOFT_DEADLINE" env-description:"Slot-relative deadline (ms into slot) for the MEV-optimized proposal-collection window. Leave unset for the default (legacy relative-timeout) path; set explicitly to opt into the MEV-optimized path (value must be in [1000ms, 1450ms]; higher values up to 3600ms require AllowDangerousProposalSoftDeadline). Cannot be combined with ProposerDelay or ProposalSoftTimeout. See https://github.com/ssvlabs/ssv/blob/main/docs/MEV_CONSIDERATIONS.md for details."`

	// AllowDangerousProposalSoftDeadline lifts the ProposalSoftDeadline safe-max cap (~1450ms) up
	// to the hard maximum (3600ms). Without it, a ProposalSoftDeadline above the safe-max is
	// rejected at startup, because the worst-case 2-round QBFT scenario may not fit within the slot
	// (an explicit "round 1 must succeed" configuration). Mirrors AllowDangerousProposerDelay.
	// See docs/MEV_CONSIDERATIONS.md.
	AllowDangerousProposalSoftDeadline bool `yaml:"AllowDangerousProposalSoftDeadline" env:"ALLOW_DANGEROUS_PROPOSAL_SOFT_DEADLINE" env-description:"Allow ProposalSoftDeadline values above the safe-max (~1450ms) up to the hard maximum (3600ms). Dangerous: the worst-case 2-round QBFT fallback may not fit within the slot, risking missed proposals. See https://github.com/ssvlabs/ssv/blob/main/docs/MEV_CONSIDERATIONS.md for details."`
}
