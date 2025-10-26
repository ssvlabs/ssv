package exporter

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/exporter"
)

// ValidatorTracesRequest represents the filter parameters accepted by the
// validator traces endpoints.
type ValidatorTracesRequest struct {
	// From is the starting slot (inclusive).
	From uint64 `json:"from" example:"123456" format:"int64" minimum:"0"`
	// To is the ending slot (inclusive).
	To uint64 `json:"to" example:"123460" format:"int64" minimum:"0"`
	// Roles is a comma-separated list of beacon roles to include.
	Roles api.RoleSlice `json:"roles" swaggertype:"array,string" enums:"ATTESTER,AGGREGATOR,PROPOSER,SYNC_COMMITTEE,SYNC_COMMITTEE_CONTRIBUTION" binding:"required"`
	// PubKeys is a comma-separated list of validator public keys (hex, 96 chars per key).
	PubKeys api.HexSlice `json:"pubkeys" swaggertype:"array,string" format:"hex" minLength:"96" maxLength:"96" pattern:"^[0-9a-f]{96}$"`
	// Indices is a comma-separated list of validator indices.
	Indices api.Uint64Slice `json:"indices" swaggertype:"array,integer" format:"int64" minimum:"0"`
}

// pubKeys implements the filterRequest interface.
func (r *ValidatorTracesRequest) pubKeys() []spectypes.ValidatorPK {
	return parsePubkeysSlice(r.PubKeys)
}

// indices implements the filterRequest interface.
func (r *ValidatorTracesRequest) indices() []uint64 {
	return r.Indices
}

// hasFilters implements the filterRequest interface.
func (r *ValidatorTracesRequest) hasFilters() bool {
	return len(r.PubKeys) > 0 || len(r.Indices) > 0
}

// ValidatorTracesResponse represents the API response returned by the
// validator traces endpoints.
type ValidatorTracesResponse struct {
	// Data contains the list of validator duty traces matching the request.
	Data []ValidatorTrace `json:"data"`
	// Schedule lists requested duties per validator within the requested slot range.
	Schedule []ValidatorSchedule `json:"schedule"`
	// Errors lists non-fatal issues encountered while building the response (duties not found, enrichment errors, etc.).
	Errors []string `json:"errors,omitempty" swaggertype:"array,string" example:"duty data unavailable for slot 123457"`
}

// ValidatorTrace captures the consensus trace information for a single
// validator duty.
type ValidatorTrace struct {
	// Slot is the duty slot for this validator trace (numeric string).
	Slot phase0.Slot `json:"slot" swaggertype:"string" example:"123456"`
	// Role is the beacon role for this duty (e.g., ATTESTER).
	Role string `json:"role" example:"ATTESTER"`
	// Validator is the validator index for the duty (numeric string).
	Validator phase0.ValidatorIndex `json:"validator" swaggertype:"string" example:"123"`
	// CommitteeID is the 32-byte committee identifier (hex), when applicable.
	CommitteeID string `json:"committeeID,omitempty" format:"hex" example:"aabbcc"`
	// Rounds lists per-round QBFT messages observed for this validator.
	Rounds []Round `json:"consensus"`
	// Decideds lists decided messages emitted for this duty.
	Decideds []Decided `json:"decideds"`
	// Pre lists pre-consensus partial signature messages.
	Pre []Message `json:"pre"`
	// Post lists post-consensus partial signature messages.
	Post []Message `json:"post"`
	// Proposal is the hex-encoded proposal payload for this duty, if available.
	Proposal string `json:"proposalData,omitempty" format:"hex"`
}

func toValidatorTrace(t *exporter.ValidatorDutyTrace) ValidatorTrace {
	return ValidatorTrace{
		Slot:      t.Slot,
		Role:      t.Role.String(),
		Validator: t.Validator,
		Pre:       toMessageTrace(t.Pre),
		Post:      toMessageTrace(t.Post),
		Rounds:    toRounds(t.Rounds),
		Proposal:  formatProposalData(t.ProposalData),
		Decideds:  toDecideds(t.Decideds),
	}
}

type validatorDutyTraceWithCommitteeID struct {
	exporter.ValidatorDutyTrace
	CommitteeID *spectypes.CommitteeID
}

// ValidatorSchedule is a compact, user-friendly representation of scheduled duties
// for a single validator at a given slot.
type ValidatorSchedule struct {
	Slot      uint64   `json:"slot" format:"int64"`
	Validator uint64   `json:"validator" format:"int64"`
	Roles     []string `json:"roles"`
}
