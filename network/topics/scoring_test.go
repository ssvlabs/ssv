package topics

import (
	"fmt"
	"testing"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
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
		"ssv.v2.110": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 1,
		},
		"ssv.v2.114": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 1,
		},
		"ssv.v2.117": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 2,
		},
		"ssv.v2.118": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 2,
		},
		"ssv.v2.119": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 1,
		},
		"ssv.v2.122": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 1,
		},
		"ssv.v2.123": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 3,
		},
		"ssv.v2.124": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 2,
		},
		"ssv.v2.126": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 1,
		},
		"ssv.v2.13": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 1,
		},
		"ssv.v2.17": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 3,
		},
		"ssv.v2.18": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 3,
		},
		"ssv.v2.22": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 4,
		},
		"ssv.v2.23": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 4,
		},
		"ssv.v2.25": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 1,
		},
		"ssv.v2.26": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 1,
		},
		"ssv.v2.3": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 1,
		},
		"ssv.v2.35": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 1,
		},
		"ssv.v2.38": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 1,
		},
		"ssv.v2.4": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 3,
		},
		"ssv.v2.40": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 1,
		},
		"ssv.v2.41": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 1,
		},
		"ssv.v2.42": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 3,
		},
		"ssv.v2.45": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 2,
		},
		"ssv.v2.46": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 2,
		},
		"ssv.v2.48": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 3,
		},
		"ssv.v2.49": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 1,
		},
		"ssv.v2.5": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 4,
		},
		"ssv.v2.50": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 1,
		},
		"ssv.v2.52": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 4,
		},
		"ssv.v2.55": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 2,
		},
		"ssv.v2.58": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 2,
		},
		"ssv.v2.59": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 3,
		},
		"ssv.v2.6": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 6,
		},
		"ssv.v2.60": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 4,
		},
		"ssv.v2.61": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 4,
		},
		"ssv.v2.62": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 2,
		},
		"ssv.v2.64": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 3,
		},
		"ssv.v2.66": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 3,
		},
		"ssv.v2.69": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 1,
		},
		"ssv.v2.70": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 4,
		},
		"ssv.v2.72": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 1,
		},
		"ssv.v2.73": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 5,
		},
		"ssv.v2.76": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 1,
		},
		"ssv.v2.8": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 2,
		},
		"ssv.v2.81": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 2,
		},
		"ssv.v2.83": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 2,
		},
		"ssv.v2.84": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 1,
		},
		"ssv.v2.89": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 1,
		},
		"ssv.v2.9": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 2,
		},
		"ssv.v2.91": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 3,
		},
		"ssv.v2.92": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 3,
		},
		"ssv.v2.93": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 5,
		},
		"ssv.v2.94": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 2,
		},
		"ssv.v2.97": {
			TimeInMesh:               0,
			FirstMessageDeliveries:   0,
			MeshMessageDeliveries:    0,
			InvalidMessageDeliveries: 1,
		},
	}

	logs := truncateStats(filtered)
	fmt.Println(logs)
}
