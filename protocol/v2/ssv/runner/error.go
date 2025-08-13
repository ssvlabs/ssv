package runner

import (
	"fmt"
)

var (
	ErrNoValidDutiesToExecute = fmt.Errorf("no valid duties to execute")

	// Below is a list of retryable errors a runner might encounter.

	// ErrNoRunningDuty means we might not have started the duty yet, while another operator already did + sent this
	// message to us.
	ErrNoRunningDuty = fmt.Errorf("no running duty")
	// ErrFuturePartialSigMsg means the message we've got is "from the future"; it can happen if we haven't advanced
	// the runner to the slot the message is targeting yet, while another operator already did + sent this message
	// to us.
	ErrFuturePartialSigMsg = fmt.Errorf("future partial sig msg")
	// ErrInstanceNotFound means we might not have started the QBFT instance yet, while another operator already did
	// + sent this message to us.
	ErrInstanceNotFound = fmt.Errorf("instance not found")
	// ErrFutureConsensusMsg means the message we've got is "from the future"; it can happen if we haven't started
	// QBFT instance for the slot the message is targeting yet, while another operator already did + sent this
	// message to us.
	ErrFutureConsensusMsg = fmt.Errorf("future consensus msg")
	// ErrWrongMsgHeight means the message we've got is targeting QBFT height that's different from the one our
	// QBFT instance is targeting, not sure how/why this happens, but for now we'll replay this message in case
	// this is similar to "future message" case.
	ErrWrongMsgHeight = fmt.Errorf("wrong msg height")
	// ErrNoProposalForRound means we might not have received a proposal-message yet, while another operator already
	// did and started preparing it.
	ErrNoProposalForRound = fmt.Errorf("no proposal for round")
	// ErrWrongMsgRound means we might not have changed the round to the next one yet, while another operator already
	// did + sent this message to us.
	ErrWrongMsgRound = fmt.Errorf("wrong msg round")
	// ErrNoDecidedValue means we might not have finished QBFT consensus phase yet, while another operator already
	// did + sent this message to us.
	ErrNoDecidedValue = fmt.Errorf("no decided value")
)
