package params

import (
	"encoding/json"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/stretchr/testify/require"
	"net"
	"testing"
	"time"
)

func TestTopicScoreParams(t *testing.T) {
	tests := []struct {
		name        string
		opts        func() *Options
		expectedErr error
	}{
		{
			"decided topic 1k validators",
			func() *Options {
				opts := NewDecidedTopicOpts(1000, 128)
				return &opts
			},
			nil,
		},
		{
			"decided topic 10k validators",
			func() *Options {
				opts := NewDecidedTopicOpts(10000, 128)
				return &opts
			},
			nil,
		},
		{
			"subnet topic 1k validators",
			func() *Options {
				opts := NewSubnetTopicOpts(1000, 128)
				return &opts
			},
			nil,
		},
		{
			"subnet topic 10k validators",
			func() *Options {
				opts := NewSubnetTopicOpts(10000, 128)
				return &opts
			},
			nil,
		},
		{
			"subnet topic 100k validators",
			func() *Options {
				opts := NewSubnetTopicOpts(100000, 128)
				return &opts
			},
			nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := test.opts()
			topicScoreParams, err := TopicParams(*opts)
			require.NoError(t, err)
			require.NotNil(t, topicScoreParams)
			raw, err := json.Marshal(topicScoreParams)
			require.NoError(t, err)
			require.NotNil(t, raw)
			t.Log("topic score params:\n", string(raw))
		})
	}
}

func TestPeerScoreParams(t *testing.T) {
	peerScoreParams := PeerScoreParams(oneEpochDuration, 550*(time.Millisecond*700), 0, 0)
	raw, err := peerScoreParamsString(peerScoreParams)
	require.NoError(t, err)
	require.NotNil(t, raw)
	t.Log("peer score params:\n", raw)
}

func peerScoreParamsString(psp *pubsub.PeerScoreParams) (string, error) {
	cp := peerScoreParamsSerializable{
		TopicScoreCap:               psp.TopicScoreCap,
		AppSpecificWeight:           psp.AppSpecificWeight,
		IPColocationFactorWeight:    psp.IPColocationFactorWeight,
		IPColocationFactorThreshold: psp.IPColocationFactorThreshold,
		IPColocationFactorWhitelist: psp.IPColocationFactorWhitelist,
		BehaviourPenaltyWeight:      psp.BehaviourPenaltyWeight,
		BehaviourPenaltyThreshold:   psp.BehaviourPenaltyThreshold,
		BehaviourPenaltyDecay:       psp.BehaviourPenaltyDecay,
		DecayInterval:               psp.DecayInterval,
		DecayToZero:                 psp.DecayToZero,
		RetainScore:                 psp.RetainScore,
		SeenMsgTTL:                  psp.SeenMsgTTL,
	}
	raw, err := json.Marshal(&cp)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

type peerScoreParamsSerializable struct {
	//Topics map[string]*TopicScoreParams
	TopicScoreCap                                                            float64
	AppSpecificWeight                                                        float64
	IPColocationFactorWeight                                                 float64
	IPColocationFactorThreshold                                              int
	IPColocationFactorWhitelist                                              []*net.IPNet
	BehaviourPenaltyWeight, BehaviourPenaltyThreshold, BehaviourPenaltyDecay float64
	DecayInterval                                                            time.Duration
	DecayToZero                                                              float64
	RetainScore                                                              time.Duration
	SeenMsgTTL                                                               time.Duration
}
