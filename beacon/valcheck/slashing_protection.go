package valcheck

// SlashingProtection is a controller for different types of ethereum value and slashing protection instances
type SlashingProtection struct {
}

// New returns a new instance of slashing protection
func New() *SlashingProtection {
	return &SlashingProtection{}
}

// AttestationSlashingProtector returns an attestation slashing protection value check
func (sp *SlashingProtection) AttestationSlashingProtector() *AttestationValueCheck {
	return &AttestationValueCheck{}
}

// ProposalSlashingProtector returns a proposal slashing protection value check
func (sp *SlashingProtection) ProposalSlashingProtector() *ProposerValueCheck {
	return &ProposerValueCheck{}
}

// AggregationValidation returns an aggregation value check
func (sp *SlashingProtection) AggregationValidation() *AggregatorValueCheck {
	return &AggregatorValueCheck{}
}
