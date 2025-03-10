package ekm

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/eth2-key-manager/core"
	slashingprotection "github.com/ssvlabs/eth2-key-manager/slashing_protection"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"
)

const (
	// MinSPAttestationEpochGap is the minimum epoch distance used for slashing protection in attestations.
	// It defines the smallest allowable gap between the source and target epochs in an existing attestation
	// and those in a new attestation, helping to prevent slashable offenses.
	MinSPAttestationEpochGap = phase0.Epoch(0)
	// MinSPProposalSlotGap is the minimum slot distance used for slashing protection in block proposals.
	// It defines the smallest allowable gap between the current slot and the slot of a new block proposal,
	// helping to prevent slashable offenses.
	MinSPProposalSlotGap = phase0.Slot(0)
)

type SlashingProtector interface {
	// TODO: consider using phase0.BLSPubKey for public keys
	ListAccounts() ([]core.ValidatorAccount, error)
	RetrieveHighestAttestation(pubKey []byte) (*phase0.AttestationData, bool, error)
	RetrieveHighestProposal(pubKey []byte) (phase0.Slot, bool, error)
	RemoveHighestAttestation(pubKey []byte) error
	RemoveHighestProposal(pubKey []byte) error
	IsAttestationSlashable(pk spectypes.ShareValidatorPK, data *phase0.AttestationData) error
	IsBeaconBlockSlashable(pk []byte, slot phase0.Slot) error
	UpdateHighestAttestation(pubKey []byte, attestation *phase0.AttestationData) error
	UpdateHighestProposal(pubKey []byte, slot phase0.Slot) error
	BumpSlashingProtection(pubKey []byte) error
}

type slashingProtector struct {
	logger      *zap.Logger
	signerStore Storage
	protection  *slashingprotection.NormalProtection
}

func NewSlashingProtector(
	logger *zap.Logger,
	signerStore Storage,
	protection *slashingprotection.NormalProtection,
) SlashingProtector {
	return &slashingProtector{
		logger:      logger,
		signerStore: signerStore,
		protection:  protection,
	}
}

func (sp *slashingProtector) ListAccounts() ([]core.ValidatorAccount, error) {
	return sp.signerStore.ListAccounts()
}

func (sp *slashingProtector) RetrieveHighestAttestation(pubKey []byte) (*phase0.AttestationData, bool, error) {
	return sp.signerStore.RetrieveHighestAttestation(pubKey)
}

func (sp *slashingProtector) RetrieveHighestProposal(pubKey []byte) (phase0.Slot, bool, error) {
	return sp.signerStore.RetrieveHighestProposal(pubKey)
}

func (sp *slashingProtector) RemoveHighestAttestation(pubKey []byte) error {
	return sp.signerStore.RemoveHighestAttestation(pubKey)
}

func (sp *slashingProtector) RemoveHighestProposal(pubKey []byte) error {
	return sp.signerStore.RemoveHighestProposal(pubKey)
}

func (sp *slashingProtector) IsAttestationSlashable(pk spectypes.ShareValidatorPK, data *phase0.AttestationData) error {
	if val, err := sp.protection.IsSlashableAttestation(pk, data); err != nil || val != nil {
		if err != nil {
			return err
		}
		return fmt.Errorf("slashable attestation (%s), not signing", val.Status)
	}
	return nil
}

func (sp *slashingProtector) IsBeaconBlockSlashable(pk []byte, slot phase0.Slot) error {
	status, err := sp.protection.IsSlashableProposal(pk, slot)
	if err != nil {
		return err
	}
	if status.Status != core.ValidProposal {
		return fmt.Errorf("slashable proposal (%s), not signing", status.Status)
	}

	return nil
}

func (sp *slashingProtector) UpdateHighestAttestation(pubKey []byte, attestation *phase0.AttestationData) error {
	return sp.protection.UpdateHighestAttestation(pubKey, attestation)
}

func (sp *slashingProtector) UpdateHighestProposal(pubKey []byte, slot phase0.Slot) error {
	return sp.protection.UpdateHighestProposal(pubKey, slot)
}

// BumpSlashingProtection updates the slashing protection data for a given public key.
func (sp *slashingProtector) BumpSlashingProtection(pubKey []byte) error {
	currentSlot := sp.signerStore.BeaconNetwork().EstimatedCurrentSlot()

	// Update highest attestation data for slashing protection.
	if err := sp.updateHighestAttestation(pubKey, currentSlot); err != nil {
		return err
	}

	// Update highest proposal data for slashing protection.
	if err := sp.updateHighestProposal(pubKey, currentSlot); err != nil {
		return err
	}

	return nil
}

// updateHighestAttestation updates the highest attestation data for slashing protection.
func (sp *slashingProtector) updateHighestAttestation(pubKey []byte, slot phase0.Slot) error {
	// Retrieve the highest attestation data stored for the given public key.
	retrievedHighAtt, found, err := sp.RetrieveHighestAttestation(pubKey)
	if err != nil {
		return fmt.Errorf("could not retrieve highest attestation: %w", err)
	}

	currentEpoch := sp.signerStore.BeaconNetwork().EstimatedEpochAtSlot(slot)
	minimalSP := sp.computeMinimalAttestationSP(currentEpoch)

	// Check if the retrieved highest attestation data is valid and not outdated.
	if found && retrievedHighAtt != nil {
		if retrievedHighAtt.Source.Epoch >= minimalSP.Source.Epoch || retrievedHighAtt.Target.Epoch >= minimalSP.Target.Epoch {
			return nil
		}
	}

	// At this point, either the retrieved attestation data was not found, or it was outdated.
	// In either case, we update it to the minimal slashing protection data.
	if err := sp.signerStore.SaveHighestAttestation(pubKey, minimalSP); err != nil {
		return fmt.Errorf("could not save highest attestation: %w", err)
	}

	return nil
}

// updateHighestProposal updates the highest proposal slot for slashing protection.
func (sp *slashingProtector) updateHighestProposal(pubKey []byte, slot phase0.Slot) error {
	// Retrieve the highest proposal slot stored for the given public key.
	retrievedHighProp, found, err := sp.RetrieveHighestProposal(pubKey)
	if err != nil {
		return fmt.Errorf("could not retrieve highest proposal: %w", err)
	}

	minimalSPSlot := sp.computeMinimalProposerSP(slot)

	// Check if the retrieved highest proposal slot is valid and not outdated.
	if found && retrievedHighProp != 0 {
		if retrievedHighProp >= minimalSPSlot {
			return nil
		}
	}

	// At this point, either the retrieved proposal slot was not found, or it was outdated.
	// In either case, we update it to the minimal slashing protection slot.
	if err := sp.signerStore.SaveHighestProposal(pubKey, minimalSPSlot); err != nil {
		return fmt.Errorf("could not save highest proposal: %w", err)
	}

	return nil
}

// computeMinimalAttestationSP calculates the minimal safe attestation data for slashing protection.
// It takes the current epoch as an argument and returns an AttestationData object with the minimal safe source and target epochs.
func (sp *slashingProtector) computeMinimalAttestationSP(epoch phase0.Epoch) *phase0.AttestationData {
	// Calculate the highest safe target epoch based on the current epoch and a predefined minimum distance.
	highestTarget := epoch + MinSPAttestationEpochGap
	// The highest safe source epoch is one less than the highest target epoch.
	highestSource := highestTarget - 1

	// Return a new AttestationData object with the calculated source and target epochs.
	return &phase0.AttestationData{
		Source: &phase0.Checkpoint{
			Epoch: highestSource,
		},
		Target: &phase0.Checkpoint{
			Epoch: highestTarget,
		},
	}
}

// computeMinimalProposerSP calculates the minimal safe slot for a block proposal to avoid slashing.
// It takes the current slot as an argument and returns the minimal safe slot.
func (sp *slashingProtector) computeMinimalProposerSP(slot phase0.Slot) phase0.Slot {
	// Calculate the highest safe proposal slot based on the current slot and a predefined minimum distance.
	return slot + MinSPProposalSlotGap
}
