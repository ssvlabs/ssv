package valcheck

import "github.com/bloxapp/ssv/beacon"

// SlashingProtection is a controller for different types of ethereum value and slashing protection instances
type SlashingProtection struct {
	signer beacon.Signer
}

// New returns a new instance of slashing protection
func New(signer beacon.Signer) *SlashingProtection {
	return &SlashingProtection{signer: signer}
}

// AttestationSlashingProtector returns an attestation slashing protection value check
func (sp *SlashingProtection) AttestationSlashingProtector() *AttestationValueCheck {
	return &AttestationValueCheck{signer: sp.signer}
}

// ProposalSlashingProtector returns a proposal slashing protection value check
func (sp *SlashingProtection) ProposalSlashingProtector() *ProposerValueCheck {
	return &ProposerValueCheck{}
}

// AggregationValidation returns an aggregation value check
func (sp *SlashingProtection) AggregationValidation() *AggregatorValueCheck {
	return &AggregatorValueCheck{}
}
