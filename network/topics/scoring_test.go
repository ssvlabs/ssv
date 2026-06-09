package topics

import (
	"testing"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	zapobserver "go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/network/peers/peertrace"
)

func TestTruncateStats(t *testing.T) {
	// Test empty.
	filtered := []*topicScoreSnapshot{}
	log := formatInvalidMessageStats(filtered)
	require.Equal(t, "", log)

	// Test few subnets.
	filtered = []*topicScoreSnapshot{
		{
			"custom_topic",
			&pubsub.TopicScoreSnapshot{
				TimeInMesh:               0,
				FirstMessageDeliveries:   0,
				MeshMessageDeliveries:    0,
				InvalidMessageDeliveries: 3,
			},
		},
		{
			"ssv.v2.103",
			&pubsub.TopicScoreSnapshot{
				TimeInMesh:               0,
				FirstMessageDeliveries:   0,
				MeshMessageDeliveries:    0,
				InvalidMessageDeliveries: 3,
			},
		},
		{
			"ssv.v2.107",
			&pubsub.TopicScoreSnapshot{
				TimeInMesh:               3 * time.Second,
				FirstMessageDeliveries:   3.5,
				MeshMessageDeliveries:    2.666666,
				InvalidMessageDeliveries: 3.83,
			},
		},
		{
			"ssv.v2.109",
			&pubsub.TopicScoreSnapshot{
				TimeInMesh:               -20 * time.Millisecond,
				FirstMessageDeliveries:   -3.333333,
				MeshMessageDeliveries:    -2.25,
				InvalidMessageDeliveries: -3.83,
			},
		},
	}
	log = formatInvalidMessageStats(filtered)
	require.Equal(t, "custom_topic=0,0,0,3 103=0,0,0,3 107=3,3.5,2.67,3.83 109=-0.02,-3.33,-2.25,-3.83", log)
}

func TestScoreInspectorObservesHighlightedPeersBetweenLogCycles(t *testing.T) {
	highlightedPID, err := peer.Decode("12D3KooWGRZpEouTWybB5jDKsVLqYXn3hXyzuTNxti4ghui6u5HE")
	require.NoError(t, err)
	regularPID, err := peer.Decode("12D3KooWAVdZV4v1YKiB8icTQmeqHvRsVSVNqZ3iJ1Ls5C1xe6NC")
	require.NoError(t, err)

	observer, err := peertrace.New(peertrace.Config{Peers: highlightedPID.String()})
	require.NoError(t, err)

	core, logs := zapobserver.New(zap.DebugLevel)
	logger := zap.New(core)
	inspector := scoreInspector(
		t.Context(),
		logger,
		nil,
		2,
		func(peer.ID) bool { return true },
		&pubsub.PeerScoreParams{
			IPColocationFactorWeight: -1,
			BehaviourPenaltyWeight:   -1,
		},
		func(string) *pubsub.TopicScoreParams {
			return &pubsub.TopicScoreParams{
				TopicWeight:                    1,
				TimeInMeshWeight:               1,
				TimeInMeshQuantum:              time.Second,
				TimeInMeshCap:                  10,
				FirstMessageDeliveriesWeight:   1,
				InvalidMessageDeliveriesWeight: -1,
			}
		},
		&testGossipScoreIndex{},
		observer,
	)

	scores := map[peer.ID]*pubsub.PeerScoreSnapshot{
		highlightedPID: testPeerScoreSnapshot("ssv.v2.1"),
		regularPID:     testPeerScoreSnapshot("ssv.v2.1"),
	}

	inspector(scores)
	require.Len(t, logs.FilterMessage("peer scores").All(), 2)
	require.Len(t, logs.FilterMessage("p2p highlighted peer event").All(), 1)
	logs.TakeAll()

	inspector(scores)
	require.Empty(t, logs.FilterMessage("peer scores").All())
	require.Len(t, logs.FilterMessage("p2p highlighted peer event").All(), 1)
	require.Equal(t, highlightedPID.String(), logs.All()[0].ContextMap()["peer_id"])
}

func testPeerScoreSnapshot(topic string) *pubsub.PeerScoreSnapshot {
	return &pubsub.PeerScoreSnapshot{
		Score:              -12,
		IPColocationFactor: 1,
		BehaviourPenalty:   2,
		Topics: map[string]*pubsub.TopicScoreSnapshot{
			topic: {
				TimeInMesh:               2 * time.Second,
				FirstMessageDeliveries:   3,
				InvalidMessageDeliveries: 4,
			},
		},
	}
}

type testGossipScoreIndex struct {
	scores map[peer.ID]float64
}

func (i *testGossipScoreIndex) SetScores(scores map[peer.ID]float64) {
	i.scores = scores
}

func (i *testGossipScoreIndex) GetGossipScore(peerID peer.ID) (float64, bool) {
	score, ok := i.scores[peerID]
	return score, ok
}

func (i *testGossipScoreIndex) HasBadGossipScore(peerID peer.ID) (bool, float64) {
	score, ok := i.GetGossipScore(peerID)
	return ok && score < 0, score
}
