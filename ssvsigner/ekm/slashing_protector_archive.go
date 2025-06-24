package ekm

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// SlashingProtectionArchiver handles archiving and restoring slashing protection data
// for validators across re-registration cycles.
//
// TODO(SSV-15): This interface is part of the temporary solution for audit finding SSV-15.
// The underlying issue is that validator share regeneration produces new public keys,
// making existing slashing protection data (keyed by share pubkey) inaccessible upon re-registration.
//
// The current workaround archives slashing protection by validator pubkey during removal and
// restores it during re-addition. This addresses an edge case where a majority fork (like Holesky)
// could lead to slashing if validators are removed and re-added.
//
// Long-term solutions under consideration:
// 1. Deterministic share generation based on validator key
// 2. Validator-centric slashing protection storage (align with EIP-3076)
// 3. Consistent key derivation scheme for share keys
//
// This interface should be removed once a proper architectural solution is implemented.
type SlashingProtectionArchiver interface {
	// ArchiveSlashingProtection preserves slashing protection data keyed by validator public key.
	// This method is part of the temporary solution for audit finding SSV-15.
	// It should be called before removing a validator share to maintain slashing protection
	// history when the validator is subsequently re-added with regenerated shares.
	//
	// TODO(SSV-15): Remove this method once proper architectural solution is implemented.
	ArchiveSlashingProtection(validatorPubKey []byte, sharePubKey []byte) error

	// ApplyArchivedSlashingProtection applies archived slashing protection data for a validator.
	// This method is part of the temporary solution for audit finding SSV-15.
	// It should be called when adding a share to ensure slashing protection continuity
	// across validator re-registration cycles. It uses maximum value logic to prevent regression.
	//
	// TODO(SSV-15): Remove this method once proper architectural solution is implemented.
	ApplyArchivedSlashingProtection(validatorPubKey []byte, sharePubKey phase0.BLSPubKey) error
}

// applyArchivedSlashingProtection implements the core logic for applying archived slashing protection.
// This shared function is used by both LocalKeyManager and RemoteKeyManager to avoid code duplication.
//
// TODO(SSV-15): This function is part of the temporary solution for audit finding SSV-15,
// applying previously archived slashing protection history to prevent slashing after share regeneration.
// It uses maximum value logic to prevent regression.
func applyArchivedSlashingProtection(store Storage, validatorPubKey []byte, sharePubKey phase0.BLSPubKey) error {
	// Retrieve archived slashing protection data for the validator
	archive, found, err := store.RetrieveArchivedSlashingProtection(validatorPubKey)
	if err != nil {
		return fmt.Errorf("could not retrieve archived slashing protection: %w", err)
	}
	if !found || archive == nil {
		return nil // No archived data found, nothing to apply
	}

	currentSlot := store.BeaconNetwork().EstimatedCurrentSlot()
	currentEpoch := store.BeaconNetwork().EstimatedEpochAtSlot(currentSlot)

	// Apply archived attestation data using max(current, archived) logic
	if archive.HighestAttestation != nil {
		// Get current slashing protection data for the share
		currentAtt, foundCurrent, err := store.RetrieveHighestAttestation(sharePubKey[:])
		if err != nil {
			return fmt.Errorf("could not retrieve current highest attestation: %w", err)
		}

		var targetEpoch, sourceEpoch phase0.Epoch
		if foundCurrent && currentAtt != nil {
			// Use max of current and archived values
			if currentAtt.Target.Epoch > archive.HighestAttestation.Target.Epoch {
				targetEpoch = currentAtt.Target.Epoch
			} else {
				targetEpoch = archive.HighestAttestation.Target.Epoch
			}

			if currentAtt.Source.Epoch > archive.HighestAttestation.Source.Epoch {
				sourceEpoch = currentAtt.Source.Epoch
			} else {
				sourceEpoch = archive.HighestAttestation.Source.Epoch
			}
		} else {
			// Use max of current epoch and archived values
			if currentEpoch > archive.HighestAttestation.Target.Epoch {
				targetEpoch = currentEpoch
			} else {
				targetEpoch = archive.HighestAttestation.Target.Epoch
			}

			if targetEpoch > 0 {
				sourceEpoch = targetEpoch - 1
			}
			if currentEpoch > 0 && currentEpoch-1 > archive.HighestAttestation.Source.Epoch {
				sourceEpoch = currentEpoch - 1
			} else if archive.HighestAttestation.Source.Epoch > sourceEpoch {
				sourceEpoch = archive.HighestAttestation.Source.Epoch
			}
		}

		// Create new attestation data with the computed epochs
		newAttData := &phase0.AttestationData{
			Source: &phase0.Checkpoint{Epoch: sourceEpoch},
			Target: &phase0.Checkpoint{Epoch: targetEpoch},
		}

		// Save the updated attestation data
		if err := store.SaveHighestAttestation(sharePubKey[:], newAttData); err != nil {
			return fmt.Errorf("could not save updated highest attestation: %w", err)
		}
	}

	// Apply archived proposal data using max(current, archived) logic
	if archive.HighestProposal > 0 {
		// Get the current highest proposal for the share
		currentProp, foundProp, err := store.RetrieveHighestProposal(sharePubKey[:])
		if err != nil {
			return fmt.Errorf("could not retrieve current highest proposal: %w", err)
		}

		var highestSlot phase0.Slot
		if foundProp && currentProp > archive.HighestProposal {
			highestSlot = currentProp
		} else if currentSlot > archive.HighestProposal {
			highestSlot = currentSlot
		} else {
			highestSlot = archive.HighestProposal
		}

		// Save the updated proposal data
		if err := store.SaveHighestProposal(sharePubKey[:], highestSlot); err != nil {
			return fmt.Errorf("could not save updated highest proposal: %w", err)
		}
	}

	return nil
}
