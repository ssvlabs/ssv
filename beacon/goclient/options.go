package goclient

import (
	"fmt"
	"time"

	"github.com/ssvlabs/ssv/networkconfig"
)

// Options defines beacon client options
type Options struct {
	BeaconConfig                *networkconfig.Beacon
	BeaconNodeAddr              string `yaml:"BeaconNodeAddr" env:"BEACON_NODE_ADDR" env-required:"true" env-description:"Beacon node URL(s). Multiple nodes are supported via semicolon-separated URLs (e.g. 'http://localhost:5052;http://localhost:5053')"`
	SyncDistanceTolerance       uint64 `yaml:"SyncDistanceTolerance" env:"BEACON_SYNC_DISTANCE_TOLERANCE" env-default:"4" env-description:"Maximum number of slots behind head considered in-sync"`
	WithWeightedAttestationData bool   `yaml:"WithWeightedAttestationData" env:"WITH_WEIGHTED_ATTESTATION_DATA" env-default:"false" env-description:"Enable attestation data scoring across multiple beacon nodes"`
	WithParallelSubmissions     bool   `yaml:"WithParallelSubmissions" env:"WITH_PARALLEL_SUBMISSIONS" env-default:"false" env-description:"Enables parallel Attestation and Sync Committee submissions to all Beacon nodes (as opposed to submitting to a single Beacon node via multiclient instance)"`

	CommonTimeout time.Duration // Optional.
	LongTimeout   time.Duration // Optional.

	ProposalSoftTimeout time.Duration `yaml:"ProposalSoftTimeout" env:"WITH_PROPOSAL_SOFT_TIMEOUT" env-description:"Specifies the beacon proposal collection soft timeout; it will be adjusted for the proposer delay"`
	ProposalHardTimeout time.Duration `yaml:"ProposalHardTimeout" env:"WITH_PROPOSAL_HARD" env-description:"Specifies the beacon proposal collection hard timeout; it will be adjusted for the proposer delay"`
}

func NewOptions(base Options, proposerDelay time.Duration) (Options, error) {
	options := base
	if options.ProposalSoftTimeout == 0 {
		options.ProposalSoftTimeout = DefaultProposalSoftTimeout
	}

	if options.ProposalHardTimeout == 0 {
		options.ProposalHardTimeout = DefaultProposalHardTimeout
	}

	if proposerDelay > 0 {
		options.ProposalSoftTimeout -= proposerDelay
		options.ProposalHardTimeout -= proposerDelay
	}

	if options.ProposalSoftTimeout < 0 {
		return Options{}, fmt.Errorf("invalid proposal soft timeout: %s", options.ProposalSoftTimeout)
	}

	if options.ProposalHardTimeout < options.ProposalSoftTimeout ||
		options.ProposalHardTimeout < 0 {
		return Options{}, fmt.Errorf("invalid proposal hard timeout: %s", options.ProposalHardTimeout)
	}

	return options, nil
}
