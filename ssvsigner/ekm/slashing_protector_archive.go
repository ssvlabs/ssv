package ekm

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/storage/basedb"
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
	ArchiveSlashingProtection(txn basedb.Txn, validatorPubKey []byte, sharePubKey []byte) error

	// ApplyArchivedSlashingProtection applies archived slashing protection data for a validator.
	// This method is part of the temporary solution for audit finding SSV-15.
	// It should be called when adding a share to ensure slashing protection continuity
	// across validator re-registration cycles. It uses maximum value logic to prevent regression.
	//
	// TODO(SSV-15): Remove this method once proper architectural solution is implemented.
	ApplyArchivedSlashingProtection(txn basedb.Txn, validatorPubKey []byte, sharePubKey phase0.BLSPubKey) error
}
