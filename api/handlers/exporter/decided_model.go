package exporter

import (
	"encoding/hex"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/ibft/storage"
)

// DecidedParticipant describes a decided duty participant entry.
type DecidedParticipant struct {
	Role      string `json:"role" example:"ATTESTER"`
	Slot      uint64 `json:"slot" format:"int64" example:"123456"`
	PublicKey string `json:"public_key" format:"hex"`
	Message   struct {
		// We're keeping "Signers" capitalized to avoid breaking existing clients that rely on the current structure.
		Signers []uint64 `json:"Signers"`
	} `json:"message"`
}

// TraceDecidedsResponse represents the payload returned by the TraceDecideds endpoint.
type TraceDecidedsResponse struct {
	// Data contains the decided duty participant entries matching the request.
	Data []*DecidedParticipant `json:"data"`
	// Errors lists non-fatal issues encountered while building the response (e.g., entries with empty signer sets).
	Errors []string `json:"errors,omitempty" swaggertype:"array,string" example:"omitting entry with no signers (index=deadbeef, slot=123456, role=ATTESTER)"`
}

// DecidedsResponse represents the payload returned by the backward-compatible Decideds endpoint.
type DecidedsResponse struct {
	// Data contains the decided duty participant entries.
	Data []*DecidedParticipant `json:"data"`
	// Errors lists non-fatal issues encountered while building the response.
	Errors []string `json:"errors,omitempty" swaggertype:"array,string" example:"error getting participants: timeout"`
}

// TraceDecidedsResponseFromParticipants builds a TraceDecidedsResponse from the given participants and errors slice.
func TraceDecidedsResponseFromParticipants(participants []*DecidedParticipant, errors []string) TraceDecidedsResponse {
	return TraceDecidedsResponse{
		Data:   participants,
		Errors: errors,
	}
}

type DecidedsRequest struct {
	// From is the starting slot (inclusive).
	From uint64 `json:"from" format:"int64" minimum:"0"`
	// To is the ending slot (inclusive).
	To uint64 `json:"to" format:"int64" minimum:"0"`
	// Roles is a comma-separated list of beacon roles to include.
	Roles api.RoleSlice `json:"roles" swaggertype:"array,string" enums:"ATTESTER,AGGREGATOR,PROPOSER,SYNC_COMMITTEE,SYNC_COMMITTEE_CONTRIBUTION" binding:"required"`
	// PubKeys is a comma-separated list of validator public keys (hex, 96 chars per key).
	PubKeys api.HexSlice `json:"pubkeys" swaggertype:"array,string" format:"hex" minLength:"96" maxLength:"96" pattern:"^[0-9a-f]{96}$"`
	// Indices is a comma-separated list of validator indices.
	Indices api.Uint64Slice `json:"indices" swaggertype:"array,integer" format:"int64" minimum:"0"`
}

// implements filterRequest interface
func (r *DecidedsRequest) pubKeys() []spectypes.ValidatorPK {
	return parsePubkeysSlice(r.PubKeys)
}

// implements filterRequest interface
func (r *DecidedsRequest) indices() []uint64 {
	return r.Indices
}

// implements filterRequest interface
func (r *DecidedsRequest) hasFilters() bool {
	return len(r.PubKeys) > 0 || len(r.Indices) > 0
}

func toParticipantResponse(role spectypes.BeaconRole, entry storage.ParticipantsRangeEntry) *DecidedParticipant {
	response := &DecidedParticipant{
		Role:      role.String(),
		Slot:      uint64(entry.Slot),
		PublicKey: hex.EncodeToString(entry.PubKey[:]),
	}
	response.Message.Signers = entry.Signers

	return response
}
