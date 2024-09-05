package topics

import (
	"testing"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/stretchr/testify/require"
)

func TestTruncateStats(t *testing.T) {
	filtered := map[string]*pubsub.TopicScoreSnapshot{
		"ssv.v2.103": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 3,
		},
		"ssv.v2.107": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 3,
		},
		"ssv.v2.109": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 1,
		},
	}

	logs := truncateStats(filtered)
	require.Equal(t, []string{"ssv.v2.103=0.000;0.000;3.000;", "ssv.v2.107=0.000;0.000;3.000;", "ssv.v2.109=0.000;0.000;1.000;"}, logs)
}
