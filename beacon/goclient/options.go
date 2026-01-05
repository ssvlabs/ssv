package goclient

import (
	"time"

	"github.com/ssvlabs/ssv/networkconfig"
)

const (
	// Client timeouts.
	DefaultCommonTimeout = time.Second * 5  // For dialing and most requests.
	DefaultLongTimeout   = time.Second * 60 // For long requests.

	// Proposal timeouts
	DefaultProposalTimeout = time.Millisecond * 1600
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

	ProposalTimeout time.Duration `yaml:"ProposalTimeout" env:"WITH_PROPOSAL_SOFT_TIMEOUT" env-description:"Specifies the beacon proposal collection timeout, after proposer delay has ellapsed"`
}

func NewOptions(base Options) (Options, error) {
	options := base

	if options.CommonTimeout == 0 {
		options.CommonTimeout = DefaultCommonTimeout
	}

	if options.LongTimeout == 0 {
		options.LongTimeout = DefaultLongTimeout
	}

	if options.ProposalTimeout == 0 {
		options.ProposalTimeout = DefaultProposalTimeout
	}

	return options, nil
}
