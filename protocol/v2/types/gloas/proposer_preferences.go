package gloas

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// Regenerate with `go generate ./...`. The phase0/bellatrix --include is resolved from the module
// graph (`go list -m`), so it tracks go-eth2-client across dependency bumps rather than pinning.
//go:generate sh -c "go tool -modfile=../../../../tool.mod sszgen -path ./proposer_preferences.go --include $(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/phase0,$(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/bellatrix --objs ProposerPreferences,SignedProposerPreferences"

// ProposerPreferences is the Gloas (ePBS) preference a proposer broadcasts for an upcoming
// proposal slot (SIP #94 §5): the fee recipient and target gas limit builders must honor, pinned to
// the proposer-lookahead seed via DependentRoot. Signed under DomainProposerPreferences with domain
// epoch = epoch(ProposalSlot). Fixed 76-byte SSZ.
type ProposerPreferences struct {
	DependentRoot  phase0.Root `ssz-size:"32"`
	ProposalSlot   phase0.Slot
	ValidatorIndex phase0.ValidatorIndex
	FeeRecipient   bellatrix.ExecutionAddress `ssz-size:"20"`
	TargetGasLimit uint64
}

// SignedProposerPreferences is a ProposerPreferences plus the validator's signature, broadcast on
// the proposer_preferences gossip topic. Fixed 172-byte SSZ.
type SignedProposerPreferences struct {
	Message   *ProposerPreferences
	Signature phase0.BLSSignature `ssz-size:"96"`
}

// proposerPreferencesJSON is the beacon-API JSON form: uint64 as a decimal string, root/address as
// 0x-hex, per go-eth2-client conventions.
type proposerPreferencesJSON struct {
	DependentRoot  string `json:"dependent_root"`
	ProposalSlot   string `json:"proposal_slot"`
	ValidatorIndex string `json:"validator_index"`
	FeeRecipient   string `json:"fee_recipient"`
	TargetGasLimit string `json:"target_gas_limit"`
}

// MarshalJSON implements json.Marshaler.
func (p *ProposerPreferences) MarshalJSON() ([]byte, error) {
	return json.Marshal(&proposerPreferencesJSON{
		DependentRoot:  fmt.Sprintf("%#x", p.DependentRoot),
		ProposalSlot:   fmt.Sprintf("%d", p.ProposalSlot),
		ValidatorIndex: fmt.Sprintf("%d", p.ValidatorIndex),
		FeeRecipient:   fmt.Sprintf("%#x", p.FeeRecipient),
		TargetGasLimit: fmt.Sprintf("%d", p.TargetGasLimit),
	})
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *ProposerPreferences) UnmarshalJSON(input []byte) error {
	var data proposerPreferencesJSON
	if err := json.Unmarshal(input, &data); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if err := decodeHexInto(p.DependentRoot[:], data.DependentRoot, "dependent root"); err != nil {
		return err
	}
	proposalSlot, err := strconv.ParseUint(data.ProposalSlot, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid value for proposal slot: %w", err)
	}
	p.ProposalSlot = phase0.Slot(proposalSlot)
	validatorIndex, err := strconv.ParseUint(data.ValidatorIndex, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid value for validator index: %w", err)
	}
	p.ValidatorIndex = phase0.ValidatorIndex(validatorIndex)
	if err := decodeHexInto(p.FeeRecipient[:], data.FeeRecipient, "fee recipient"); err != nil {
		return err
	}
	targetGasLimit, err := strconv.ParseUint(data.TargetGasLimit, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid value for target gas limit: %w", err)
	}
	p.TargetGasLimit = targetGasLimit
	return nil
}

// signedProposerPreferencesJSON is the beacon-API JSON form of SignedProposerPreferences.
type signedProposerPreferencesJSON struct {
	Message   *ProposerPreferences `json:"message"`
	Signature string               `json:"signature"`
}

// MarshalJSON implements json.Marshaler.
func (s *SignedProposerPreferences) MarshalJSON() ([]byte, error) {
	return json.Marshal(&signedProposerPreferencesJSON{
		Message:   s.Message,
		Signature: fmt.Sprintf("%#x", s.Signature),
	})
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *SignedProposerPreferences) UnmarshalJSON(input []byte) error {
	var data signedProposerPreferencesJSON
	if err := json.Unmarshal(input, &data); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if data.Message == nil {
		return errors.New("message missing")
	}
	s.Message = data.Message
	if err := decodeHexInto(s.Signature[:], data.Signature, "signature"); err != nil {
		return err
	}
	return nil
}
