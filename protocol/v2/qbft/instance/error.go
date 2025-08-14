package instance

import (
	"fmt"
)

var (
	ErrNoProposalForRound = fmt.Errorf("did not receive proposal for this round")
	ErrWrongMsgRound      = fmt.Errorf("wrong msg round")
	ErrWrongMsgHeight     = fmt.Errorf("wrong msg height")
)
