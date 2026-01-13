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

	ProposalSoftTimeout time.Duration `yaml:"ProposalSoftTimeout" env:"WITH_PROPOSAL_SOFT_TIMEOUT" env-description:"Specifies the beacon proposal collection soft timeout (collection period for comparing proposals from multiple beacon nodes to select the most profitable one). Note: the 1st MEV (blinded) block is accepted immediately, so this timeout mainly affects how long we wait for an MEV block before giving up deciding to use a vanilla block instead (if we got one already). This value cannot be set any lower than 500ms to ensure there is enough time for the Beacon node to serve the block-fetch request"`
}

func NewOptions(base Options, proposerDelay time.Duration) (Options, error) {
	options := base

	if options.CommonTimeout == 0 {
		options.CommonTimeout = defaultCommonTimeout
	}

	if options.LongTimeout == 0 {
		options.LongTimeout = defaultLongTimeout
	}

	// If user explicitly set ProposalSoftTimeout, use it as-is (power user mode).
	// Otherwise, use the default value and reduce it by proposer delay if needed.
	if options.ProposalSoftTimeout == 0 {
		// The default value shouldn't be too high because an operator might not be able to participate
		// in QBFT round 2 (or finish it in time) if it is roughly > 2000 ms.
		const defaultProposalSoftTimeout = time.Millisecond * 1800
		options.ProposalSoftTimeout = defaultProposalSoftTimeout
		// Reduce soft timeout by proposer delay to maintain consistent duty-execution timelines
		// for different operators in the cluster, ensuring QBFT consensus starts at roughly
		// the same time (timing out round 1 at roughly the same time) regardless of proposer
		// delay configuration a particular operator is using - operators with higher proposer
		// delay start fetching blocks later, so they must have a shorter collection period.
		if proposerDelay > 0 {
			options.ProposalSoftTimeout -= proposerDelay
		}
	}

	// minProposalSoftTimeout is the minimum soft timeout value allowed.
	// It ensures we always have enough time to fetch and compare proposals.
	const minProposalSoftTimeout = time.Millisecond * 500
	if options.ProposalSoftTimeout < minProposalSoftTimeout {
		options.ProposalSoftTimeout = minProposalSoftTimeout
	}

	// Note: There is no hard timeout for proposals. The parent context from the
	// duty runner (bounded by slot timing) serves as the ultimate deadline.
	// This ensures we never give up early on getting a block proposal.

	return options, nil
}
