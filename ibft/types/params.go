package types

import "time"

func DefaultParams() *Params {
	return &Params{
		RoundChangeDuration: int64(time.Second * 12),
		IbftCommitteeSize:   9,
	}
}

var BasicParams = DefaultParams()
