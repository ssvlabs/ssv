package types

import "time"

func DefaultParams() *Params {
	return &Params{
		RoundChangeDuration: int64(time.Second * 2),
		IbftCommitteeSize:   3,
	}
}

var BasicParams = DefaultParams()
