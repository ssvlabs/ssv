package goclient

import (
	"time"

	"github.com/ssvlabs/ssv/networkconfig"
)

const (
	// Client timeouts.
	DefaultCommonTimeout = time.Second * 5  // For dialing and most requests.
	DefaultLongTimeout   = time.Second * 60 // For long requests.

	// DefaultProposalSoftTimeout is the base collection period during which we
	// gather proposals from multiple beacon nodes to select the best one.
	// This value is reduced by the proposer delay to maintain consistent timing.
	// After the soft timeout, we return the best proposal seen so far, or wait
	// for the first valid proposal if none received yet.
	// The parent context (duty deadline) serves as the hard timeout.
	//
	// Note: MEV (blinded) blocks return immediately, so this timeout mainly
	// affects how long we wait for MEV when we first receive a vanilla block.
	//
	// Can be overridden via WITH_PROPOSAL_SOFT_TIMEOUT env var. When explicitly
	// set, the value is used as-is without proposer delay reduction (power user mode).
	DefaultProposalSoftTimeout = time.Millisecond * 1800

	// MinProposalSoftTimeout is the minimum collection period, even with maximum
	// proposer delay. This ensures we always have some time to compare proposals.
	MinProposalSoftTimeout = time.Millisecond * 500
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

	ProposalSoftTimeout time.Duration `yaml:"ProposalSoftTimeout" env:"WITH_PROPOSAL_SOFT_TIMEOUT" env-description:"Specifies the beacon proposal collection soft timeout (collection period for comparing proposals from multiple beacon nodes)"`
}

func NewOptions(base Options, proposerDelay time.Duration) (Options, error) {
	options := base

	if options.CommonTimeout == 0 {
		options.CommonTimeout = DefaultCommonTimeout
	}

	if options.LongTimeout == 0 {
		options.LongTimeout = DefaultLongTimeout
	}

	// If user explicitly set ProposalSoftTimeout, use it as-is (power user mode).
	// Otherwise, use default and reduce by proposer delay.
	if options.ProposalSoftTimeout == 0 {
		options.ProposalSoftTimeout = DefaultProposalSoftTimeout

		// Reduce soft timeout by proposer delay to maintain consistent timing.
		// Users with proposer delay start fetching later, so they get a shorter
		// collection period. This ensures consensus starts at roughly the same
		// time regardless of proposer delay configuration.
		//
		// Examples (with default 1800ms soft timeout):
		//   - 0ms delay    → 1800ms collection
		//   - 500ms delay  → 1300ms collection
		//   - 1000ms delay → 800ms collection
		//   - 1300ms delay → 500ms collection (capped at minimum)
		if proposerDelay > 0 {
			options.ProposalSoftTimeout -= proposerDelay
			if options.ProposalSoftTimeout < MinProposalSoftTimeout {
				options.ProposalSoftTimeout = MinProposalSoftTimeout
			}
		}
	}

	// Note: There is no hard timeout for proposals. The parent context from the
	// duty runner (bounded by slot timing) serves as the ultimate deadline.
	// This ensures we never give up early on getting a block proposal.

	return options, nil
}
