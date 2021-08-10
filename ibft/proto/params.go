package proto

//DefaultConsensusParams returns the default round change duration time
func DefaultConsensusParams() *InstanceConfig {
	return &InstanceConfig{
		RoundChangeDurationSeconds:   2,
		LeaderPreprepareDelaySeconds: 1,
	}
}
