package storage

import (
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// ValidatorID represents a generic identifier for validators that can be either
// a public key or an index.
type ValidatorID interface {
	// String returns a human-readable representation of the validator ID.
	// Format: "pubkey:0x..." for public keys, "index:12345" for indices.
	String() string

	// Equal checks if two ValidatorIDs represent the same validator.
	Equal(other ValidatorID) bool

	// isValidatorID is a marker method that ensures only specific types
	// can implement this interface.
	isValidatorID()
}

// ValidatorPubKey represents a validator identified by its BLS public key.
// This is a 48-byte value that uniquely identifies a validator.
type ValidatorPubKey spectypes.ValidatorPK

// isValidatorID implements the ValidatorID interface.
func (ValidatorPubKey) isValidatorID() {}

// String returns the hex-encoded public key prefixed with "pubkey:".
func (v ValidatorPubKey) String() string {
	return fmt.Sprintf("pubkey:0x%s", hex.EncodeToString(v[:]))
}

// Equal checks if this public key equals another ValidatorID.
func (v ValidatorPubKey) Equal(other ValidatorID) bool {
	if other == nil {
		return false
	}

	otherPK, ok := other.(ValidatorPubKey)
	if !ok {
		return false
	}

	return v == otherPK
}

// Bytes returns the raw bytes of the public key.
func (v ValidatorPubKey) Bytes() []byte {
	return v[:]
}

// ToValidatorPK converts to the spectypes.ValidatorPK type.
func (v ValidatorPubKey) ToValidatorPK() spectypes.ValidatorPK {
	return spectypes.ValidatorPK(v)
}

// ValidatorIndex represents a validator identified by its index on the beacon chain.
// Indices are assigned when validators are activated and are more efficient for
// lookups than public keys.
type ValidatorIndex phase0.ValidatorIndex

// isValidatorID implements the ValidatorID interface.
func (ValidatorIndex) isValidatorID() {}

// String returns the index as a decimal string prefixed with "index:".
func (v ValidatorIndex) String() string {
	return fmt.Sprintf("index:%d", uint64(v))
}

// Equal checks if this index equals another ValidatorID.
func (v ValidatorIndex) Equal(other ValidatorID) bool {
	if other == nil {
		return false
	}

	otherIdx, ok := other.(ValidatorIndex)
	if !ok {
		return false
	}

	return v == otherIdx
}

// Uint64 returns the index as a uint64.
func (v ValidatorIndex) Uint64() uint64 {
	return uint64(v)
}

// ToPhase0 converts to the phase0.ValidatorIndex type.
func (v ValidatorIndex) ToPhase0() phase0.ValidatorIndex {
	return phase0.ValidatorIndex(v)
}

// NewValidatorPubKey creates a ValidatorID from a public key.
// Returns an error if the public key is not exactly 48 bytes.
func NewValidatorPubKey(pubKey []byte) (ValidatorID, error) {
	if len(pubKey) != 48 {
		return nil, fmt.Errorf("invalid public key length: got %d, expected 48", len(pubKey))
	}

	var pk spectypes.ValidatorPK
	copy(pk[:], pubKey)

	return ValidatorPubKey(pk), nil
}

// NewValidatorIndex creates a ValidatorID from an index.
func NewValidatorIndex(index uint64) ValidatorID {
	return ValidatorIndex(index)
}

// ParseValidatorID parses a string representation of a ValidatorID.
// Accepts formats: "pubkey:0x..." or "index:12345".
func ParseValidatorID(s string) (ValidatorID, error) {
	if len(s) < 7 { // Minimum: "index:0"
		return nil, fmt.Errorf("invalid validator ID format: %s", s)
	}

	switch {
	case len(s) > 7 && s[:7] == "pubkey:":
		hexStr := s[7:]
		// Remove 0x prefix if present
		if len(hexStr) >= 2 && hexStr[:2] == "0x" {
			hexStr = hexStr[2:]
		}

		pubKey, err := hex.DecodeString(hexStr)
		if err != nil {
			return nil, fmt.Errorf("invalid hex in validator pubkey: %w", err)
		}

		return NewValidatorPubKey(pubKey)

	case len(s) > 6 && s[:6] == "index:":
		indexStr := s[6:]
		index, err := strconv.ParseUint(indexStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid validator index: %w", err)
		}

		return NewValidatorIndex(index), nil

	default:
		return nil, fmt.Errorf("unknown validator ID format: %s", s)
	}
}

// IsValidatorPubKey checks if the ValidatorID is a public key type.
func IsValidatorPubKey(id ValidatorID) bool {
	_, ok := id.(ValidatorPubKey)

	return ok
}

// IsValidatorIndex checks if the ValidatorID is an index type.
func IsValidatorIndex(id ValidatorID) bool {
	_, ok := id.(ValidatorIndex)

	return ok
}
