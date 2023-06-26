package eventdatahandler

import (
	"github.com/bloxapp/ssv/eth1_refactor/eventdb"
)

type eventDB interface {
	ROTxn() eventdb.RO
	RWTxn() eventdb.RW
}
