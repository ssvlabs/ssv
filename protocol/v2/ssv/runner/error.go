package runner

import (
	"fmt"
)

var (
	ErrNoValidDutiesToExecute = fmt.Errorf("no valid duties to execute")

	// Below is a list of retryable errors.

	ErrNoRunningDuty         = fmt.Errorf("no running duty")
	ErrInvalidPartialSigSlot = fmt.Errorf("invalid partial sig slot")
	ErrInstanceNotFound      = fmt.Errorf("instance not found")
	ErrFutureMsg             = fmt.Errorf("future msg")
	ErrWrongMsgHeight        = fmt.Errorf("wrong msg height")
	ErrNoProposalForRound    = fmt.Errorf("no proposal for round")
	ErrWrongMsgRound         = fmt.Errorf("wrong msg round")
	ErrNoDecidedValue        = fmt.Errorf("no decided value")
)
