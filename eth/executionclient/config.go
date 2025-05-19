package executionclient

import (
	"time"

	"github.com/ssvlabs/ssv/networkconfig"
)

// TODO: rename eth1, consider combining with consensus client options

// Options contains config configurations related to Ethereum execution client.
type Options struct {
	Addr                  string        `yaml:"ETH1Addr" env:"ETH_1_ADDR" env-required:"true" env-description:"Execution client WebSocket URL(s). Multiple clients are supported via semicolon-separated URLs (e.g. 'ws://localhost:8546;ws://localhost:8547')"`
	ConnectionTimeout     time.Duration `yaml:"ETH1ConnectionTimeout" env:"ETH_1_CONNECTION_TIMEOUT" env-default:"10s" env-description:"Timeout for execution client connections"`
	SyncDistanceTolerance uint64        `yaml:"ETH1SyncDistanceTolerance" env:"ETH_1_SYNC_DISTANCE_TOLERANCE" env-default:"5" env-description:"Maximum number of blocks behind head considered in-sync"`
}

type Config struct {
	SlotsPerEpoch          uint64 // Slots per epoch
	FinalityConsensusEpoch uint64 // Epoch at which finality fork activates
	FollowDistance         uint64 // Number of blocks to follow behind head
}

// NewConfigFromNetwork creates a new Config with network-specific values
// and default values for other parameters.
func NewConfigFromNetwork(networkCfg networkconfig.NetworkConfig) Config {
	return Config{
		SlotsPerEpoch:          networkCfg.SlotsPerEpoch(),
		FinalityConsensusEpoch: networkCfg.FinalityConsensusEpoch,
		FollowDistance:         DefaultFollowDistance,
	}
}

func (c Config) WithSlotsPerEpoch(slots uint64) Config {
	c.SlotsPerEpoch = slots
	return c
}

func (c Config) WithFinalityConsensusEpoch(epoch uint64) Config {
	c.FinalityConsensusEpoch = epoch
	return c
}

func (c Config) WithFollowDistance(distance uint64) Config {
	c.FollowDistance = distance
	return c
}
