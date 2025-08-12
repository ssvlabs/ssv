package controller

import (
	"fmt"
)

var (
	ErrInstanceNotFound   = fmt.Errorf("instance not found")
	ErrFutureMsg          = fmt.Errorf("future msg from height, could not process")
	ErrWrongMsgHeight     = fmt.Errorf("wrong msg height")
	ErrNoProposalForRound = fmt.Errorf("no proposal for round")
	ErrWrongMsgRound      = fmt.Errorf("wrong msg round")
)
