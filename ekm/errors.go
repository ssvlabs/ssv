package ekm

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/eth2-key-manager/core"
)

type SlashableAttestationError struct {
	Status                 core.VoteDetectionType
	AttestationSourceEpoch phase0.Epoch
	AttestationTargetEpoch phase0.Epoch
	HighestSourceEpoch     phase0.Epoch
	HighestTargetEpoch     phase0.Epoch
}

func (se SlashableAttestationError) Error() string {
	return fmt.Sprintf("slashable attestation (%s), not signing (debug: ASE %v ATE %v HSE %v HTE %v)", se.Status, se.AttestationSourceEpoch, se.AttestationTargetEpoch, se.HighestSourceEpoch, se.HighestTargetEpoch)
}

type SlashableProposalError struct {
	Status core.ProposalDetectionType
}

func (se SlashableProposalError) Error() string {
	return fmt.Sprintf("slashable proposal (%s), not signing", se.Status)
}
