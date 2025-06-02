package params

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/registry/storage"
)

func TestTopicScoreParams(t *testing.T) {
	tests := []struct {
		name        string
		opts        func() *Options
		expectedErr error
	}{
		{
			"subnet topic 0 validators",
			func() *Options {
				validators := uint64(0)
				opts := NewSubnetTopicOpts(networkconfig.TestNetwork, validators, 128, []*storage.IndexedCommittee{})
				return opts
			},
			nil,
		},
		{
			"subnet topic 1k validators",
			func() *Options {
				validators := uint64(1000)
				opts := NewSubnetTopicOpts(networkconfig.TestNetwork, validators, 128, createTestingSingleCommittees(validators))
				return opts
			},
			nil,
		},
		{
			"subnet topic 10k validators",
			func() *Options {
				validators := uint64(10_000)
				opts := NewSubnetTopicOpts(networkconfig.TestNetwork, validators, 128, createTestingSingleCommittees(validators))
				return opts
			},
			nil,
		},
		{
			"subnet topic 51k validators",
			func() *Options {
				validators := uint64(51_000)
				opts := NewSubnetTopicOpts(networkconfig.TestNetwork, validators, 128, createTestingSingleCommittees(validators))
				return opts
			},
			nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := test.opts()
			// raw, err := json.MarshalIndent(&opts, "", "\t")
			raw, err := json.Marshal(&opts)
			require.NoError(t, err)
			t.Logf("[%s] using opts:\n%s", test.name, string(raw))
			topicScoreParams, err := TopicParams(opts)
			require.NoError(t, err)
			require.NotNil(t, topicScoreParams)
			// raw, err = json.MarshalIndent(topicScoreParams, "", "\t")
			raw, err = json.Marshal(topicScoreParams)
			require.NoError(t, err)
			require.NotNil(t, raw)
			t.Logf("[%s] topic score params:\n%s", test.name, string(raw))
		})
	}
}

func TestPeerScoreParams(t *testing.T) {
	peerScoreParams := PeerScoreParams(networkconfig.TestNetwork, 550*(time.Millisecond*700), false)
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
	// raw, err := json.MarshalIndent(&cp, "", "\t")
	raw, err := json.Marshal(&cp)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

type peerScoreParamsSerializable struct {
	// Topics map[string]*TopicScoreParams
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
