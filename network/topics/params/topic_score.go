package params

import (
	"math"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/pkg/errors"
)

const (
	// Network Topology
	gossipSubD          = 8
	minActiveValidators = 200

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
	// Mesh scording is disabled for now.
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
	// ErrOperatorsAndValidatorsWithDifferentLength is returned in case the length of the operators and validators map are different
	ErrOperatorsAndValidatorsWithDifferentLength = errors.New("operators and validators map with different length")
	// ErrDifferentCommittees is returned in case the operators and validators map have different committees as keys
	ErrDifferentCommittees = errors.New("operators and validators with different committees")
)

// NetworkOpts is the config struct for network configurations
type NetworkOpts struct {
	// CommitteeOperators is a map from a CommitteeID to its number of operators
	CommitteeOperators map[string]int `json:"-"`
	// CommitteeValidators is a map from a CommitteeID to its number of validators
	CommitteeValidators map[string]int `json:"-"`
	// ActiveValidators is the amount of validators in the network
	ActiveValidators int
	// Subnets is the number of subnets in the network
	Subnets int
	// OneEpochDuration is used as a time-frame length to control scoring in a dynamic way
	OneEpochDuration time.Duration
	// TotalTopicsWeight is the weight of all the topics in the network
	TotalTopicsWeight float64
}

// Returns the list of number of operators and validators for committees
func (n *NetworkOpts) GetOperatorsAndValidatorsCountList() ([]int, []int, error) {
	operators := make([]int, 0)
	validators := make([]int, 0)
	// Get the number of operators and validators for each committee, in order
	for committee, numOperators := range n.CommitteeOperators {
		numValidators, exists := n.CommitteeValidators[committee]
		if !exists {
			return nil, nil, ErrDifferentCommittees
		}
		operators = append(operators, numOperators)
		validators = append(validators, numValidators)
	}

	return operators, validators, nil
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

func (o *Options) validate() error {
	if o.Network.ActiveValidators < minActiveValidators {
		return ErrLowValidatorsCount
	}

	// Validate operators and validators map
	if len(o.Network.CommitteeOperators) != len(o.Network.CommitteeValidators) {
		return ErrOperatorsAndValidatorsWithDifferentLength
	}
	for committee := range o.Network.CommitteeOperators {
		if _, exists := o.Network.CommitteeValidators[committee]; !exists {
			return ErrDifferentCommittees
		}
	}

	return nil
}

// maxScore attainable by a peer
func (o *Options) maxScore() float64 {
	return (o.Topic.MaxTimeInMeshScore + o.Topic.MaxFirstDeliveryScore) * o.Network.TotalTopicsWeight
}

// NewOpts creates new TopicOpts instance
func NewOpts(activeValidators, subnets int, committeeOperators map[string]int, committeeValidators map[string]int) *Options {
	return &Options{
		Network: NetworkOpts{
			ActiveValidators:    activeValidators,
			Subnets:             subnets,
			CommitteeOperators:  committeeOperators,
			CommitteeValidators: committeeValidators,
		},
		Topic: TopicOpts{},
	}
}

// NewSubnetTopicOpts creates new TopicOpts for a subnet topic
func NewSubnetTopicOpts(activeValidators, subnets int, committeeOperators map[string]int, committeeValidators map[string]int) (*Options, error) {
	// Create options with default values
	opts := NewOpts(activeValidators, subnets, committeeOperators, committeeValidators)
	opts.defaults()

	// Validate options
	if err := opts.validate(); err != nil {
		return nil, err
	}

	// Set topic weight with equal weights
	opts.Topic.TopicWeight = opts.Network.TotalTopicsWeight / float64(opts.Network.Subnets)

	// Set the expected message rate for the topic
	operators, validators, err := opts.Network.GetOperatorsAndValidatorsCountList()
	if err != nil {
		return nil, err
	}
	opts.Topic.ExpectedMsgRate, err = calculateMessageRateForTopic(operators, validators)
	if err != nil {
		return nil, err
	}

	return opts, nil
}

// TopicParams creates pubsub.TopicScoreParams from the given TopicOpts
// implementation is based on ETH2.0, with alignments to ssv:
// https://gist.github.com/blacktemplar/5c1862cb3f0e32a1a7fb0b25e79e6e2c
func TopicParams(opts *Options) (*pubsub.TopicScoreParams, error) {
	// Validate options
	if err := opts.validate(); err != nil {
		return nil, err
	}

	// Set to default if not set
	opts.defaults()

	expectedMessagesPerDecayInterval := opts.Topic.ExpectedMsgRate * decayInterval.Seconds()

	// P1
	timeInMeshCap := float64(opts.Topic.TimeInMeshQuantumCap) / float64(opts.Topic.TimeInMeshQuantum)

	// P2
	firstMessageDeliveriesDecay := scoreDecay(opts.Network.OneEpochDuration*opts.Topic.FirstDeliveryDecayEpochs, decayInterval)
	firstMessageDeliveriesCap, err := decayConvergence(firstMessageDeliveriesDecay, 2*(expectedMessagesPerDecayInterval)/float64(opts.Topic.D))
	if err != nil {
		return nil, errors.Wrap(err, "could not calculate decay convergence for first message delivery cap")
	}

	// P3
	meshMessageDeliveriesDecay := scoreDecay(opts.Network.OneEpochDuration*opts.Topic.MeshDeliveryDecayEpochs, decayInterval)
	meshMessageDeliveriesThreshold, err := decayThreshold(meshMessageDeliveriesDecay, (expectedMessagesPerDecayInterval * opts.Topic.MeshDeliveryDampeningFactor))
	if err != nil {
		return nil, errors.Wrap(err, "could not calculate threshold for mesh message deliveries threshold")
	}
	var meshMessageDeliveriesWeight float64
	if meshScoringEnabled {
		meshMessageDeliveriesWeight = -(opts.maxScore() / (opts.Topic.TopicWeight * math.Pow(meshMessageDeliveriesThreshold, 2)))
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

	return params, nil
}
