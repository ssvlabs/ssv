package types

import "time"

func DefaultParams() *Params {
	return &Params{
		RoundChangeDuration: int64(time.Second * 12),
		IbftCommitteeSize:   4,
	}
}

var BasicParams = DefaultParams()
