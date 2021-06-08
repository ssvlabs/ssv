package proto

import "time"

//DefaultConsensusParams returns the default round change duration time
func DefaultConsensusParams() *InstanceConfig {
	return &InstanceConfig{
		RoundChangeDuration:   int64(time.Second * 3),
		LeaderPreprepareDelay: int64(time.Second * 1),
	}
}
