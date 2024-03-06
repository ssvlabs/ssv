package ekm

import (
	"fmt"

	"github.com/bloxapp/eth2-key-manager/core"
)

type SlashableAttestationError struct {
	Status core.VoteDetectionType
}

func (se SlashableAttestationError) Error() string {
	return fmt.Sprintf("slashable attestation (%s), not signing", se.Status)
}

type SlashableProposalError struct {
	Status core.ProposalDetectionType
}

func (se SlashableProposalError) Error() string {
	return fmt.Sprintf("slashable proposal (%s), not signing", se.Status)
}
