package params

import (
	"math"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/pkg/errors"
)

const (
	gossipSubD       = 8
	oneEpochDuration = (12 * time.Second) * 32
	slotsPerEpoch    = 32
	// maxInMeshScore describes the max score a peer can attain from being in the mesh
	maxInMeshScore = 10
	// maxFirstDeliveryScore describes the max score a peer can obtain from first deliveries
	maxFirstDeliveryScore = 40
	// decayToZero specifies the terminal value that we will use when decaying a value.
	// decayToZero = 0.01
	// dampeningFactor reduces the amount by which the various thresholds and caps are created.
	// using value of 50 (prysm changed to 90)
	dampeningFactor = 50

	subnetTopicsWeight          = 4.0
	totalTopicsWeigth           = 4.0
	invalidMeshDeliveriesWeight = -800

	minActiveValidators = 200

	// P1
	timeInMeshQuantum    = time.Second * 12
	timeInMeshQuantumCap = 3600
	timeInMeshMaxScore   = 10

	// P2
	expectedMessagesPerSec       = 600
	maxFirstMessageDeliveryScore = 40

	// P3
	meshMessageDeliveriesDampeningFactor = 1 / 50
	meshMessageDeliveriesCapFactor       = 16
)

var (
	// ErrLowValidatorsCount is returned in case the amount of validators is not sufficient
	// for calculating score params
	ErrLowValidatorsCount = errors.New("low validators count")
)

// NetworkOpts is the config struct for network configurations
type NetworkOpts struct {
	// ActiveValidators is the amount of validators in the network
	ActiveValidators int
	// Subnets is the number of subnets in the network
	Subnets int
	//// Groups is the amount of groups used in the network
	// Groups int
	// OneEpochDuration is used as a time-frame length to control scoring in a dynamic way
	OneEpochDuration time.Duration
	// TotalTopicsWeight is the weight of all the topics in the network
	TotalTopicsWeight float64
}

// TopicOpts is the config struct for topic configurations
type TopicOpts struct {
	// TopicWeight is the weight of the topic
	TopicWeight float64
	//  ExpectedMsgRate is the expected rate for the topic
	ExpectedMsgRate       float64
	InvalidMsgDecayTime   time.Duration
	FirstMsgDecayTime     time.Duration
	MeshMsgDecayTime      time.Duration
	MeshMsgCapFactor      float64
	MeshMsgActivationTime time.Duration
	// D is the gossip degree
	D int
}

// Options is the struct used for creating topic score params
type Options struct {
	Network NetworkOpts
	Topic   TopicOpts
}

func (o *Options) defaults() {
	if o.Network.OneEpochDuration == 0 {
		o.Network.OneEpochDuration = oneEpochDuration
	}
	if o.Network.TotalTopicsWeight == 0 {
		o.Network.TotalTopicsWeight = subnetTopicsWeight // + ...
	}
	if o.Topic.D == 0 {
		o.Topic.D = gossipSubD
	}
}

func (o *Options) validate() error {
	if o.Network.ActiveValidators < minActiveValidators {
		return ErrLowValidatorsCount
	}
	return nil
}

// maxScore attainable by a peer
func (o *Options) maxScore() float64 {
	return (maxInMeshScore + maxFirstDeliveryScore) * o.Network.TotalTopicsWeight
}

// NewOpts creates new TopicOpts instance with defaults
func NewOpts(activeValidators, subnets int) Options {
	return Options{
		Network: NetworkOpts{
			ActiveValidators: activeValidators,
			Subnets:          subnets,
		},
		Topic: TopicOpts{},
	}
}

// NewSubnetTopicOpts creates new TopicOpts for a subnet topic
func NewSubnetTopicOpts(activeValidators, subnets int) Options {
	opts := NewOpts(activeValidators, subnets)
	opts.defaults()
	opts.Topic.TopicWeight = subnetTopicsWeight / float64(opts.Network.Subnets)
	validatorsPerSubnet := float64(opts.Network.ActiveValidators) / float64(opts.Network.Subnets)
	valMsgsPerEpoch := 9.0
	opts.Topic.ExpectedMsgRate = validatorsPerSubnet * valMsgsPerEpoch / float64(slotsPerEpoch)
	opts.Topic.FirstMsgDecayTime = time.Duration(8)
	opts.Topic.MeshMsgDecayTime = time.Duration(16)
	opts.Topic.MeshMsgCapFactor = 16.0 // using a large factor until we have more accurate values
	opts.Topic.MeshMsgActivationTime = opts.Network.OneEpochDuration
	return opts
}

// TopicParams creates pubsub.TopicScoreParams from the given TopicOpts
// implementation is based on ETH2.0 and prysm as a reference, with alignments to ssv:
// https://gist.github.com/blacktemplar/5c1862cb3f0e32a1a7fb0b25e79e6e2c
func TopicParams(opts Options) (*pubsub.TopicScoreParams, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	opts.defaults()

	// Topic-specific parameters

	topicWeight := totalTopicsWeigth / float64(opts.Network.Subnets)

	// P1
	timeInMeshCap := float64(timeInMeshQuantumCap / timeInMeshQuantum)

	// P2
	firstMessageDeliveriesDecay := scoreDecay(oneEpochDuration*4, decayInterval)
	firstMessageDeliveriesCap, err := decayConvergence(firstMessageDeliveriesDecay, 2*(expectedMessagesPerSec*12)/float64(opts.Topic.D))
	if err != nil {
		return nil, errors.Wrap(err, "could not calculate decay convergence for first message delivery cap")
	}

	// P3
	meshMessageDeliveriesDecay := scoreDecay(oneEpochDuration*16, decayInterval)
	meshMessageDeliveriesThreshold, err := decayThreshold(meshMessageDeliveriesDecay, math.Min(2.0, (expectedMessagesPerSec*12)*meshMessageDeliveriesDampeningFactor))
	if err != nil {
		return nil, errors.Wrap(err, "could not calculate threshold for mesh message deliveries threshold")
	}
	meshMessageDeliveriesWeight := -(maxFirstDeliveryScore + maxInMeshScore) / (topicWeight * math.Pow(meshMessageDeliveriesThreshold, 2))
	MeshMessageDeliveriesCap := meshMessageDeliveriesThreshold * meshMessageDeliveriesCapFactor

	// P4
	invalidMessageDeliveriesDecay := scoreDecay(100*oneEpochDuration, decayInterval)
	invalidMessageDeliveriesWeight := graylistThreshold / (topicWeight * 20 * 20)

	params := &pubsub.TopicScoreParams{
		// Topic-specific parameters
		TopicWeight: topicWeight,

		// P1
		TimeInMeshQuantum: timeInMeshQuantum,
		TimeInMeshCap:     timeInMeshCap,
		TimeInMeshWeight:  timeInMeshMaxScore / timeInMeshCap,

		// P2
		FirstMessageDeliveriesDecay:  firstMessageDeliveriesDecay,
		FirstMessageDeliveriesCap:    firstMessageDeliveriesCap,
		FirstMessageDeliveriesWeight: maxFirstMessageDeliveryScore / firstMessageDeliveriesCap,

		// P3
		MeshMessageDeliveriesDecay:      meshMessageDeliveriesDecay,
		MeshMessageDeliveriesThreshold:  meshMessageDeliveriesThreshold,
		MeshMessageDeliveriesWeight:     meshMessageDeliveriesWeight,
		MeshMessageDeliveriesCap:        MeshMessageDeliveriesCap,
		MeshMessageDeliveriesActivation: oneEpochDuration * 3,
		MeshMessageDeliveriesWindow:     2 * time.Second,

		// P3b
		MeshFailurePenaltyDecay:  meshMessageDeliveriesDecay,
		MeshFailurePenaltyWeight: meshMessageDeliveriesWeight,

		// P4
		InvalidMessageDeliveriesDecay:  invalidMessageDeliveriesDecay,
		InvalidMessageDeliveriesWeight: invalidMessageDeliveriesWeight,
	}

	// if opts.Topic.FirstMsgDecayTime > 0 {
	// 	params.FirstMessageDeliveriesDecay = scoreDecay(opts.Topic.FirstMsgDecayTime*opts.Network.OneEpochDuration, opts.Network.OneEpochDuration)
	// 	firstMsgDeliveryCap, err := decayConvergence(params.FirstMessageDeliveriesDecay, 2*opts.Topic.ExpectedMsgRate/float64(opts.Topic.D))
	// 	if err != nil {
	// 		return nil, errors.Wrap(err, "could not calculate first msg delivery cap")
	// 	}
	// 	params.FirstMessageDeliveriesCap = firstMsgDeliveryCap
	// 	params.FirstMessageDeliveriesWeight = maxFirstDeliveryScore / firstMsgDeliveryCap
	// }

	// if opts.Topic.MeshMsgDecayTime > 0 {
	// 	params.MeshMessageDeliveriesDecay = scoreDecay(opts.Topic.MeshMsgDecayTime*opts.Network.OneEpochDuration, opts.Network.OneEpochDuration)
	// 	// a peer must send us at least 1/50 of the regular messages in time, very conservative limit
	// 	meshMsgDeliveriesThreshold, err := decayThreshold(params.MeshMessageDeliveriesDecay, math.Min(2.0, opts.Topic.ExpectedMsgRate/dampeningFactor))
	// 	if err != nil {
	// 		return nil, errors.Wrap(err, "could not calculate mesh message deliveries threshold")
	// 	}
	// 	params.MeshMessageDeliveriesThreshold = meshMsgDeliveriesThreshold
	// 	params.MeshMessageDeliveriesCap = opts.Topic.MeshMsgCapFactor * meshMsgDeliveriesThreshold
	// 	params.MeshMessageDeliveriesWeight = -scoreByWeight(opts.maxScore(), opts.Topic.TopicWeight,
	// 		math.Max(4.0, params.MeshMessageDeliveriesCap)) // used cap instead of threshold to reduce weight
	// 	params.MeshMessageDeliveriesActivation = opts.Topic.MeshMsgActivationTime
	// 	params.MeshMessageDeliveriesWindow = 2 * time.Second
	// 	params.MeshFailurePenaltyWeight = params.MeshMessageDeliveriesWeight
	// 	params.MeshFailurePenaltyDecay = params.MeshMessageDeliveriesDecay
	// }

	// if opts.Topic.InvalidMsgDecayTime > 0 {
	// 	params.InvalidMessageDeliveriesWeight = invalidMeshDeliveriesWeight
	// 	params.InvalidMessageDeliveriesDecay = scoreDecay(opts.Topic.InvalidMsgDecayTime*opts.Network.OneEpochDuration, opts.Network.OneEpochDuration)
	// } else {
	// 	params.InvalidMessageDeliveriesDecay = 0.1
	// }

	return params, nil
}
