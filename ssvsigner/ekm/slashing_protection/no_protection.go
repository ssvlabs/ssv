package slashingprotection

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/eth2-key-manager/core"
)

// NoProtection implements slashing protector interface with dummy implementation
type NoProtection struct {
}

// IsSlashableAttestation returns always nils
func (p *NoProtection) IsSlashableAttestation(pubKey []byte, attestation *phase0.AttestationData) (*core.AttestationSlashStatus, error) {
	return nil, nil
}

// IsSlashableProposal returns always valid result
func (p *NoProtection) IsSlashableProposal(pubKey []byte, slot phase0.Slot) (*core.ProposalSlashStatus, error) {
	return &core.ProposalSlashStatus{
		Slot:   slot,
		Status: core.ValidProposal,
	}, nil
}

// UpdateHighestProposal does nothing
func (p *NoProtection) UpdateHighestProposal(pubKey []byte, slot phase0.Slot) error {
	return nil
}

// UpdateHighestAttestation does nothing
func (p *NoProtection) UpdateHighestAttestation(pubKey []byte, attestation *phase0.AttestationData) error {
	return nil
}

// FetchHighestAttestation does nothing
func (p *NoProtection) FetchHighestAttestation(pubKey []byte) (*phase0.AttestationData, bool, error) {
	return nil, false, nil
}

// FetchHighestProposal returns highest proposal data
func (p *NoProtection) FetchHighestProposal(pubKey []byte) (phase0.Slot, bool, error) {
	return 0, false, nil
}
