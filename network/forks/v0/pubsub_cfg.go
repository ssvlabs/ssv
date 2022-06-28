package v0

import "github.com/bloxapp/ssv/network/forks"

// MsgID in v0 is nil as we use the default msg_id function by libp2p
func (v0 *ForkV0) MsgID() forks.MsgIDFunc {
	return nil
}

// Subnets returns the subnets count for this fork
func (v0 *ForkV0) Subnets() int {
	return 0
}
