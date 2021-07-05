package proto

import "time"

//DefaultConsensusParams returns the default round change duration time
func DefaultConsensusParams() *InstanceConfig {
	return &InstanceConfig{
		RoundChangeDurationSeconds:   float32((time.Second * 3).Seconds()),
		LeaderPreprepareDelaySeconds: float32((time.Second * 1).Seconds()),
	}
}
