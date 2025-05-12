package params

import (
	"math"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/pkg/errors"

	"github.com/ssvlabs/ssv/registry/storage"
)

const (
	// Network Topology
	gossipSubD = 8

	// Overall parameters
	totalTopicsWeight = 4.0

	// P1
	maxTimeInMeshScore   = 10 // max score a peer can attain from being in the mesh
	timeInMeshQuantum    = 12
	timeInMeshQuantumCap = 3600

	// P2
	firstDeliveryDecayEpochs = time.Duration(4)
	maxFirstDeliveryScore    = 80 // max score a peer can obtain from first deliveries

	// P3
	// Mesh scoring is disabled for now.
	meshDeliveryDecayEpochs     = time.Duration(16)
	meshDeliveryDampeningFactor = 1.0 / 50.0
	meshDeliveryCapFactor       = 16
	meshScoringEnabled          = false

	// P4
	invalidMessageDecayEpochs = time.Duration(100)
	maxInvalidMessagesAllowed = 20
)

var (
	// ErrLowValidatorsCount is returned in case the amount of validators is not sufficient
	// for calculating score params
	ErrLowValidatorsCount = errors.New("low validators count")
)

// NetworkOpts is the config struct for network configurations
type NetworkOpts struct {
	// ActiveValidators is the amount of validators in the network
	ActiveValidators uint64
	// Subnets is the number of subnets in the network
	Subnets int
	// OneEpochDuration is used as a time-frame length to control scoring in a dynamic way
	OneEpochDuration time.Duration
	// TotalTopicsWeight is the weight of all the topics in the network
	TotalTopicsWeight float64
}

// TopicOpts is the config struct for topic configurations
type TopicOpts struct {
	// D is the gossip degree
	D int

	//  ExpectedMsgRate is the expected rate for the topic
	ExpectedMsgRate float64

	// TopicWeight is the weight of the topic
	TopicWeight float64

	// P1
	MaxTimeInMeshScore   float64
	TimeInMeshQuantum    int
	TimeInMeshQuantumCap int

	// P2
	FirstDeliveryDecayEpochs time.Duration
	MaxFirstDeliveryScore    float64

	// P3
	MeshDeliveryDecayEpochs     time.Duration
	MeshDeliveryDampeningFactor float64
	MeshDeliveryCapFactor       float64
	MeshDeliveryActivationTime  time.Duration

	// P4
	InvalidMessageDecayEpochs time.Duration
	MaxInvalidMessagesAllowed int
}

// Options is the struct used for creating topic score params
type Options struct {
	Network NetworkOpts
	Topic   TopicOpts
}

func (o *Options) defaults() {
	// Network
	if o.Network.OneEpochDuration == 0 {
		o.Network.OneEpochDuration = oneEpochDuration
	}
	if o.Network.TotalTopicsWeight == 0 {
		o.Network.TotalTopicsWeight = totalTopicsWeight
	}
	// Topic
	if o.Topic.D == 0 {
		o.Topic.D = gossipSubD
	}
	// Topic - P1
	if o.Topic.MaxTimeInMeshScore == 0 {
		o.Topic.MaxTimeInMeshScore = maxTimeInMeshScore
	}
	if o.Topic.TimeInMeshQuantum == 0 {
		o.Topic.TimeInMeshQuantum = timeInMeshQuantum
	}
	if o.Topic.TimeInMeshQuantumCap == 0 {
		o.Topic.TimeInMeshQuantumCap = timeInMeshQuantumCap
	}
	// Topic - P2
	if o.Topic.FirstDeliveryDecayEpochs == 0 {
		o.Topic.FirstDeliveryDecayEpochs = firstDeliveryDecayEpochs
	}
	if o.Topic.MaxFirstDeliveryScore == 0 {
		o.Topic.MaxFirstDeliveryScore = maxFirstDeliveryScore
	}
	// Topic - P3
	if o.Topic.MeshDeliveryDecayEpochs == 0 {
		o.Topic.MeshDeliveryDecayEpochs = meshDeliveryDecayEpochs
	}
	if o.Topic.MeshDeliveryDampeningFactor == 0 {
		o.Topic.MeshDeliveryDampeningFactor = meshDeliveryDampeningFactor
	}
	if o.Topic.MeshDeliveryCapFactor == 0 {
		o.Topic.MeshDeliveryCapFactor = meshDeliveryCapFactor
	}
	if o.Topic.MeshDeliveryActivationTime == 0 {
		o.Topic.MeshDeliveryActivationTime = o.Network.OneEpochDuration * 3
	}
	// Topic - P4
	if o.Topic.InvalidMessageDecayEpochs == 0 {
		o.Topic.InvalidMessageDecayEpochs = invalidMessageDecayEpochs
	}
	if o.Topic.MaxInvalidMessagesAllowed == 0 {
		o.Topic.MaxInvalidMessagesAllowed = maxInvalidMessagesAllowed
	}
}

// maxScore attainable by a peer
func (o *Options) maxScore() float64 {
	return (o.Topic.MaxTimeInMeshScore + o.Topic.MaxFirstDeliveryScore) * o.Network.TotalTopicsWeight
}

// NewOpts creates new TopicOpts instance
func NewOpts(activeValidators uint64, subnets int) *Options {
	return &Options{
		Network: NetworkOpts{
			ActiveValidators: activeValidators,
			Subnets:          subnets,
		},
		Topic: TopicOpts{},
	}
}

// NewSubnetTopicOpts creates new TopicOpts for a subnet topic
func NewSubnetTopicOpts(activeValidators uint64, subnets int, committees []*storage.Committee) *Options {
	// Create options with default values
	opts := NewOpts(activeValidators, subnets)
	opts.defaults()

	// Set topic weight with equal weights
	opts.Topic.TopicWeight = opts.Network.TotalTopicsWeight / float64(opts.Network.Subnets)

	// Set the expected message rate for the topic
	opts.Topic.ExpectedMsgRate = calculateMessageRateForTopic(committees)

	return opts
}

// NewSubnetTopicOpts creates new TopicOpts for a subnet topic
func NewSubnetTopicOptsValidators(activeValidators uint64, subnets int) *Options {
	// Create options with default values
	opts := NewOpts(activeValidators, subnets)
	opts.defaults()

	// Set topic weight with equal weights
	opts.Topic.TopicWeight = opts.Network.TotalTopicsWeight / float64(opts.Network.Subnets)

	// Set expected message rate based on stage metrics
	validatorsPerSubnet := float64(opts.Network.ActiveValidators) / float64(opts.Network.Subnets)
	msgsPerValidatorPerSecond := 600.0 / 10000.0
	opts.Topic.ExpectedMsgRate = validatorsPerSubnet * msgsPerValidatorPerSecond

	return opts
}

// TopicParams creates pubsub.TopicScoreParams from the given TopicOpts
// implementation is based on ETH2.0, with alignments to ssv:
// https://gist.github.com/blacktemplar/5c1862cb3f0e32a1a7fb0b25e79e6e2c
func TopicParams(opts *Options) (*pubsub.TopicScoreParams, error) {
	var err error

	// Set to default if not set
	opts.defaults()

	expectedMessagesPerDecayInterval := opts.Topic.ExpectedMsgRate * decayInterval.Seconds()

	// P1
	timeInMeshCap := float64(opts.Topic.TimeInMeshQuantumCap) / float64(opts.Topic.TimeInMeshQuantum)

	// P2
	firstMessageDeliveriesDecay := scoreDecay(opts.Network.OneEpochDuration*opts.Topic.FirstDeliveryDecayEpochs, decayInterval)
	firstMessageDeliveriesCap := 1.0
	if expectedMessagesPerDecayInterval > 0 {
		firstMessageDeliveriesCap, err = decayConvergence(firstMessageDeliveriesDecay, 2*(expectedMessagesPerDecayInterval)/float64(opts.Topic.D))
		if err != nil {
			return nil, errors.Wrap(err, "could not calculate decay convergence for first message delivery cap")
		}
	}

	// P3
	meshMessageDeliveriesDecay := scoreDecay(opts.Network.OneEpochDuration*opts.Topic.MeshDeliveryDecayEpochs, decayInterval)
	meshMessageDeliveriesThreshold := 1.0
	if expectedMessagesPerDecayInterval > 0 {
		meshMessageDeliveriesThreshold, err = decayThreshold(meshMessageDeliveriesDecay, (expectedMessagesPerDecayInterval * opts.Topic.MeshDeliveryDampeningFactor))
		if err != nil {
			return nil, errors.Wrap(err, "could not calculate threshold for mesh message deliveries threshold")
		}
	}
	var meshMessageDeliveriesWeight float64
	if meshScoringEnabled {
		meshMessageDeliveriesWeight = -(opts.maxScore() / (opts.Topic.TopicWeight * meshMessageDeliveriesThreshold * meshMessageDeliveriesThreshold))
	}
	MeshMessageDeliveriesCap := meshMessageDeliveriesThreshold * opts.Topic.MeshDeliveryCapFactor

	// P4
	invalidMessageDeliveriesDecay := scoreDecay(opts.Topic.InvalidMessageDecayEpochs*opts.Network.OneEpochDuration, decayInterval)
	invalidMessageDeliveriesWeight := graylistThreshold / (opts.Topic.TopicWeight * float64(opts.Topic.MaxInvalidMessagesAllowed) * float64(opts.Topic.MaxInvalidMessagesAllowed))

	params := &pubsub.TopicScoreParams{
		// Topic-specific parameters
		TopicWeight: opts.Topic.TopicWeight,

		// P1
		TimeInMeshQuantum: time.Duration(opts.Topic.TimeInMeshQuantum) * time.Second,
		TimeInMeshCap:     timeInMeshCap,
		TimeInMeshWeight:  opts.Topic.MaxTimeInMeshScore / timeInMeshCap,

		// P2
		FirstMessageDeliveriesDecay:  firstMessageDeliveriesDecay,
		FirstMessageDeliveriesCap:    firstMessageDeliveriesCap,
		FirstMessageDeliveriesWeight: opts.Topic.MaxFirstDeliveryScore / firstMessageDeliveriesCap,

		// P3
		MeshMessageDeliveriesDecay:      meshMessageDeliveriesDecay,
		MeshMessageDeliveriesThreshold:  meshMessageDeliveriesThreshold,
		MeshMessageDeliveriesWeight:     meshMessageDeliveriesWeight,
		MeshMessageDeliveriesCap:        MeshMessageDeliveriesCap,
		MeshMessageDeliveriesActivation: opts.Topic.MeshDeliveryActivationTime,
		MeshMessageDeliveriesWindow:     2 * time.Second,

		// P3b
		MeshFailurePenaltyDecay:  meshMessageDeliveriesDecay,
		MeshFailurePenaltyWeight: meshMessageDeliveriesWeight,

		// P4
		InvalidMessageDeliveriesDecay:  invalidMessageDeliveriesDecay,
		InvalidMessageDeliveriesWeight: invalidMessageDeliveriesWeight,
	}

	params = sanitizeTopicParams(params)

	return params, nil
}

// Sanitizes a pubsub.TopicScoreParams by assigning default values in case a parameter is NaN or Inf
func sanitizeTopicParams(params *pubsub.TopicScoreParams) *pubsub.TopicScoreParams {

	sanitizeParameter := func(value float64, defaultValue float64) float64 {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return defaultValue
		}
		return value
	}

	defaultDecay := 0.001
	defaultWeight := 0.0
	defaultCap := 1.0
	defaultThreshold := 1.0
	defaultInvalidWeight := -0.1

	// P1
	params.TimeInMeshCap = sanitizeParameter(params.TimeInMeshCap, defaultCap)
	params.TimeInMeshWeight = sanitizeParameter(params.TimeInMeshWeight, defaultWeight)

	// P2
	params.FirstMessageDeliveriesDecay = sanitizeParameter(params.FirstMessageDeliveriesDecay, defaultDecay)
	params.FirstMessageDeliveriesCap = sanitizeParameter(params.FirstMessageDeliveriesCap, defaultCap)
	params.FirstMessageDeliveriesWeight = sanitizeParameter(params.FirstMessageDeliveriesWeight, defaultWeight)

	// P3
	params.MeshMessageDeliveriesDecay = sanitizeParameter(params.MeshMessageDeliveriesDecay, defaultDecay)
	params.MeshMessageDeliveriesThreshold = sanitizeParameter(params.MeshMessageDeliveriesThreshold, defaultThreshold)
	params.MeshMessageDeliveriesWeight = sanitizeParameter(params.MeshMessageDeliveriesWeight, defaultWeight)
	params.MeshMessageDeliveriesCap = sanitizeParameter(params.MeshMessageDeliveriesCap, defaultCap)

	// P3b
	params.MeshFailurePenaltyDecay = sanitizeParameter(params.MeshFailurePenaltyDecay, defaultDecay)
	params.MeshFailurePenaltyWeight = sanitizeParameter(params.MeshFailurePenaltyWeight, defaultWeight)

	// P4
	params.InvalidMessageDeliveriesDecay = sanitizeParameter(params.InvalidMessageDeliveriesDecay, defaultDecay)
	params.InvalidMessageDeliveriesWeight = sanitizeParameter(params.InvalidMessageDeliveriesWeight, defaultInvalidWeight)

	return params
}
