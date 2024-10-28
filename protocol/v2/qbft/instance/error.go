package instance

import "fmt"

var (
	convertJustificationToProcessingMsgErr = fmt.Errorf("could not create ProcessingMessage from justification")
	noJustifiedProposalErr                 = fmt.Errorf("no justified proposal for round change quorum")
)
