package topics

import (
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"time"
)

var (
	// gsD topic stable mesh target count
	gsD = 5
	// gsDlo topic stable mesh low watermark
	gsDlo = 3
	// gsDhi topic stable mesh high watermark
	gsDhi = 9

	// gsMaxIHaveLength is max number for ihave messages to send
	// decreased the default (5000) to avoid ihave floods
	gsMaxIHaveLength = 1000
	// gsMaxIHaveMessages is the max number for ihave message to accept from a peer within a heartbeat
	// increased default (10) to avoid multiple ihave messages
	gsMaxIHaveMessages = 32
	// gsMcacheLen number of windows to retain full messages in cache for `IWANT` responses
	gsMcacheLen = 100
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
	params.MaxIHaveMessages = gsMaxIHaveMessages

	return params
}
