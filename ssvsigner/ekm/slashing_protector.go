package ekm

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/eth2-key-manager/core"
	slashingprotection "github.com/ssvlabs/eth2-key-manager/slashing_protection"
	"go.uber.org/zap"
)

// slashing_protector.go provides SlashingProtector, a wrapper around
// eth2-key-manager's NormalProtection that enforces slot/epoch
// updates (BumpSlashingProtection) preventing slashable signings.

const (
	// minSPAttestationEpochGap is the minimum epoch distance used for slashing protection in attestations.
	// It defines the smallest allowable gap between the source and target epochs in an existing attestation
	// and those in a new attestation, helping to prevent slashable offenses.
	minSPAttestationEpochGap = phase0.Epoch(0)
	// minSPProposalSlotGap is the minimum slot distance used for slashing protection in block proposals.
	// It defines the smallest allowable gap between the current slot and the slot of a new block proposal,
	// helping to prevent slashable offenses.
	minSPProposalSlotGap = phase0.Slot(0)
)

type slashingProtector interface {
	ListAccounts() ([]core.ValidatorAccount, error)
	RetrieveHighestAttestation(pubKey phase0.BLSPubKey) (*phase0.AttestationData, bool, error)
	RetrieveHighestProposal(pubKey phase0.BLSPubKey) (phase0.Slot, bool, error)
	RemoveHighestAttestation(pubKey phase0.BLSPubKey) error
	RemoveHighestProposal(pubKey phase0.BLSPubKey) error
	UpdateHighestAttestation(pubKey phase0.BLSPubKey, attData *phase0.AttestationData) error
	UpdateHighestProposal(pubKey phase0.BLSPubKey, slot phase0.Slot) error
	IsAttestationSlashable(pubKey phase0.BLSPubKey, attData *phase0.AttestationData) error
	IsBeaconBlockSlashable(pubKey phase0.BLSPubKey, slot phase0.Slot) error
	BumpSlashingProtection(pubKey phase0.BLSPubKey) error
}

// SlashingProtector manages both the local store for highest attestation/proposal
// and calls into eth2-key-manager's NormalProtection to check if an attestation
// or proposal is slashable.
type SlashingProtector struct {
	logger      *zap.Logger
	signerStore Storage
	protection  *slashingprotection.NormalProtection
}

func NewSlashingProtector(
	logger *zap.Logger,
	signerStore Storage,
	protection *slashingprotection.NormalProtection,
) *SlashingProtector {
	return &SlashingProtector{
		logger:      logger,
		signerStore: signerStore,
		protection:  protection,
	}
}

func (sp *SlashingProtector) ListAccounts() ([]core.ValidatorAccount, error) {
	return sp.signerStore.ListAccounts()
}

func (sp *SlashingProtector) RetrieveHighestAttestation(pubKey phase0.BLSPubKey) (*phase0.AttestationData, bool, error) {
	return sp.signerStore.RetrieveHighestAttestation(pubKey[:])
}

func (sp *SlashingProtector) RetrieveHighestProposal(pubKey phase0.BLSPubKey) (phase0.Slot, bool, error) {
	return sp.signerStore.RetrieveHighestProposal(pubKey[:])
}

func (sp *SlashingProtector) RemoveHighestAttestation(pubKey phase0.BLSPubKey) error {
	return sp.signerStore.RemoveHighestAttestation(pubKey[:])
}

func (sp *SlashingProtector) RemoveHighestProposal(pubKey phase0.BLSPubKey) error {
	return sp.signerStore.RemoveHighestProposal(pubKey[:])
}

func (sp *SlashingProtector) IsAttestationSlashable(pubKey phase0.BLSPubKey, attData *phase0.AttestationData) error {
	if val, err := sp.protection.IsSlashableAttestation(pubKey[:], attData); err != nil || val != nil {
		if err != nil {
			return err
		}
		return fmt.Errorf("slashable attestation (%s), not signing", val.Status)
	}
	return nil
}

func (sp *SlashingProtector) IsBeaconBlockSlashable(pubKey phase0.BLSPubKey, slot phase0.Slot) error {
	status, err := sp.protection.IsSlashableProposal(pubKey[:], slot)
	if err != nil {
		return err
	}
	if status.Status != core.ValidProposal {
		return fmt.Errorf("slashable proposal (%s), not signing", status.Status)
	}

	return nil
}

func (sp *SlashingProtector) UpdateHighestAttestation(pubKey phase0.BLSPubKey, attData *phase0.AttestationData) error {
	return sp.protection.UpdateHighestAttestation(pubKey[:], attData)
}

func (sp *SlashingProtector) UpdateHighestProposal(pubKey phase0.BLSPubKey, slot phase0.Slot) error {
	return sp.protection.UpdateHighestProposal(pubKey[:], slot)
}

// BumpSlashingProtection updates the slashing protection data for a given public key.
func (sp *SlashingProtector) BumpSlashingProtection(pubKey phase0.BLSPubKey) error {
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
func (sp *SlashingProtector) updateHighestAttestation(pubKey phase0.BLSPubKey, slot phase0.Slot) error {
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
	if err := sp.signerStore.SaveHighestAttestation(pubKey[:], minimalSP); err != nil {
		return fmt.Errorf("could not save highest attestation: %w", err)
	}

	return nil
}

// updateHighestProposal updates the highest proposal slot for slashing protection.
func (sp *SlashingProtector) updateHighestProposal(pubKey phase0.BLSPubKey, slot phase0.Slot) error {
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
	if err := sp.signerStore.SaveHighestProposal(pubKey[:], minimalSPSlot); err != nil {
		return fmt.Errorf("could not save highest proposal: %w", err)
	}

	return nil
}

// computeMinimalAttestationSP calculates the minimal safe attestation data for slashing protection.
// It takes the current epoch as an argument and returns an AttestationData object with the minimal safe source and target epochs.
func (sp *SlashingProtector) computeMinimalAttestationSP(epoch phase0.Epoch) *phase0.AttestationData {
	// Calculate the highest safe target epoch based on the current epoch and a predefined minimum distance.
	highestTarget := epoch + minSPAttestationEpochGap
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
func (sp *SlashingProtector) computeMinimalProposerSP(slot phase0.Slot) phase0.Slot {
	// Calculate the highest safe proposal slot based on the current slot and a predefined minimum distance.
	return slot + minSPProposalSlotGap
}
