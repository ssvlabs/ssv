package valcheck

// SlashingProtection is a controller for different types of ethereum value and slashing protection instances
type SlashingProtection struct {
}

// New returns a new instance of slashing protection
func New() *SlashingProtection {
	return &SlashingProtection{}
}

func (sp *SlashingProtection) AttestationSlashingProtector() *AttestationValueCheck {
	return &AttestationValueCheck{}
}

func (sp *SlashingProtection) ProposalSlashingProtector() *ProposerValueCheck {
	return &ProposerValueCheck{}
}

func (sp *SlashingProtection) AggregationValidation() *AggregatorValueCheck {
	return &AggregatorValueCheck{}
}
