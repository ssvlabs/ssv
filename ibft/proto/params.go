package proto

//DefaultConsensusParams returns the default round change duration time
func DefaultConsensusParams() *InstanceConfig {
	return &InstanceConfig{
		RoundChangeDurationSeconds:   3,
		LeaderPreprepareDelaySeconds: 1,
	}
}
