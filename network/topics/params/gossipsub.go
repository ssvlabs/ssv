package params

import (
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

const (
	// gsD topic stable mesh target count
	gsD = 8
	// gsDlo topic stable mesh low watermark
	gsDlo = 6
	// gsDhi topic stable mesh high watermark
	gsDhi = 12

	// gsMaxIHaveLength is max number for ihave messages to send
	// decreased the default (5000) to avoid ihave floods
	// TODO: increase to 2500
	gsMaxIHaveLength = 1500
	// gsMaxIHaveMessages is the max number for ihave message to accept from a peer within a heartbeat
	// TODO: decrease to 10 (default)
	gsMaxIHaveMessages = 32
	// gsMcacheLen number of windows to retain full messages in cache for `IWANT` responses
	gsMcacheLen = 6
	// gsMcacheGossip number of windows to gossip about
	gsMcacheGossip = 4

	// HeartbeatInterval interval frequency of heartbeat, milliseconds
	HeartbeatInterval = 700 * time.Millisecond
)

// GossipSubParams creates a gossipsub parameter set.
func GossipSubParams() pubsub.GossipSubParams {
	params := pubsub.DefaultGossipSubParams()
	params.Dlo = gsDlo
	params.Dhi = gsDhi
	params.D = gsD
	params.HeartbeatInterval = HeartbeatInterval
	params.HistoryLength = gsMcacheLen
	params.HistoryGossip = gsMcacheGossip
	params.MaxIHaveLength = gsMaxIHaveLength
	params.MaxIHaveMessages = gsMaxIHaveMessages

	return params
}
