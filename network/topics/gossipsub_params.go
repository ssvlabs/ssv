package topics

import (
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"time"
)

var (
	// gsD topic stable mesh target count
	gsD = 8
	// gsDlo topic stable mesh low watermark
	gsDlo = 5
	// gsDhi topic stable mesh high watermark
	gsDhi = 12

	// gsMaxIHaveLength is max number fo ihave messages to send
	// lower the maximum (default is 5000) to avoid ihave floods
	gsMaxIHaveLength = 500

	// gsMcacheLen number of windows to retain full messages in cache for `IWANT` responses
	gsMcacheLen = 15
	// gsMcacheGossip number of windows to gossip about
	gsMcacheGossip = 3

	// heartbeat interval frequency of heartbeat, milliseconds
	gsHeartbeatInterval = 700 * time.Millisecond
)

// creates a custom gossipsub parameter set.
func gossipSubParam() pubsub.GossipSubParams {
	params := pubsub.DefaultGossipSubParams()
	params.Dlo = gsDlo
	params.Dhi = gsDhi
	params.D = gsD
	params.HeartbeatInterval = gsHeartbeatInterval
	params.HistoryLength = gsMcacheLen
	params.HistoryGossip = gsMcacheGossip
	params.MaxIHaveLength = gsMaxIHaveLength

	return params
}
