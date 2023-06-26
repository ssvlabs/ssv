package eventdatahandler

import (
	"github.com/bloxapp/ssv/eth/eventdb"
)

type eventDB interface {
	ROTxn() eventdb.RO
	RWTxn() eventdb.RW
}
