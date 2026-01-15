package exporter

import (
	"encoding/hex"

	"github.com/hashicorp/go-multierror"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/api"
	exporter2 "github.com/ssvlabs/ssv/exporter2"
	"github.com/ssvlabs/ssv/ibft/storage"
)

type PubKeyLengthError struct {
	PubKey api.Hex
}

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

func toParticipantResponse(role spectypes.BeaconRole, entry storage.ParticipantsRangeEntry) *DecidedParticipant {
	response := &DecidedParticipant{
		Role:      role.String(),
		Slot:      uint64(entry.Slot),
		PublicKey: hex.EncodeToString(entry.PubKey[:]),
	}
	response.Message.Signers = entry.Signers

	return response
}

// TraceDecidedsResponse represents the payload returned by the TraceDecideds endpoint.
type TraceDecidedsResponse struct {
	// Data contains the decided duty participant entries matching the request.
	Data []*DecidedParticipant `json:"data"`
	// Errors lists non-fatal issues encountered while building the response (e.g., entries with empty signer sets).
	Errors []string `json:"errors,omitempty" swaggertype:"array,string" example:"omitting entry with no signers (index=deadbeef, slot=123456, role=ATTESTER)"`
}

// TraceDecidedsResponseFromParticipants builds a TraceDecidedsResponse from the core result and aggregated errors.
func TraceDecidedsResponseFromParticipants(result *exporter2.TraceDecidedsResult, errs *multierror.Error) TraceDecidedsResponse {
	resp := TraceDecidedsResponse{
		Data:   make([]*DecidedParticipant, 0),
		Errors: toStrings(errs),
	}

	if result == nil {
		return resp
	}

	for _, rec := range result.Participants {
		entry := storage.ParticipantsRangeEntry{
			Slot:    rec.Slot,
			PubKey:  rec.PubKey,
			Signers: rec.Signers,
		}
		resp.Data = append(resp.Data, toParticipantResponse(rec.Role, entry))
	}

	return resp
}

// DecidedsResponse represents the payload returned by the backward-compatible Decideds endpoint.
type DecidedsResponse struct {
	// Data contains the decided duty participant entries.
	Data []*DecidedParticipant `json:"data"`
	// Errors lists non-fatal issues encountered while building the response.
	Errors []string `json:"errors,omitempty" swaggertype:"array,string" example:"error getting participants: timeout"`
}

func DecidedsResponseFromResult(result *exporter2.TraceDecidedsResult) *DecidedsResponse {
	resp := &DecidedsResponse{
		Data: make([]*DecidedParticipant, 0),
	}

	if result == nil {
		return resp
	}

	for _, rec := range result.Participants {
		entry := storage.ParticipantsRangeEntry{
			Slot:    rec.Slot,
			PubKey:  rec.PubKey,
			Signers: rec.Signers,
		}
		resp.Data = append(resp.Data, toParticipantResponse(rec.Role, entry))
	}

	return resp
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

func (r *DecidedsRequest) pubKeys() []spectypes.ValidatorPK {
	return parsePubkeysSlice(r.PubKeys)
}

func toDecidedsQuery(r *DecidedsRequest) (*exporter2.DecidedsQuery, *PubKeyLengthError) {
	q := &exporter2.DecidedsQuery{
		From:  r.From,
		To:    r.To,
		Roles: toBeaconRoles(r.Roles),
	}

	// Then perform HTTP-level type validation (pubkey hex length).
	requiredLength := len(spectypes.ValidatorPK{})
	for _, req := range r.PubKeys {
		if len(req) != requiredLength {
			return nil, &PubKeyLengthError{PubKey: req}
		}
	}

	q.PubKeys = r.pubKeys()
	q.Indices = toValidatorIndices(r.Indices)

	return q, nil
}
