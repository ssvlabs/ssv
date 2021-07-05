package proto

import "time"

//DefaultConsensusParams returns the default round change duration time
func DefaultConsensusParams() *InstanceConfig {
	return &InstanceConfig{
		RoundChangeSeconds:           int64(time.Second * 3),
		LeaderPreprepareDelaySeconds: int64(time.Second * 1),
	}
}
