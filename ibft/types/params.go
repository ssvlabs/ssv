package types

import "time"

func DefaultConsensusParams() *ConsensusParams {
	return &ConsensusParams{
		RoundChangeDuration: int64(time.Second * 2),
	}
}

func (p *InstanceParams) CommitteeSize() int {
	return len(p.IbftCommittee)
}
