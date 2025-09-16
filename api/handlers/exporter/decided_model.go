package exporter

import (
	"encoding/hex"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/api"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
)

type participantResponse struct {
	Role      string `json:"role"`
	Slot      uint64 `json:"slot"`
	PublicKey string `json:"public_key"`
	Message   struct {
		// We're keeping "Signers" capitalized to avoid breaking existing clients that rely on the current structure
		Signers []uint64 `json:"Signers"`
	} `json:"message"`
}

type decidedResponse struct {
	Data   []*participantResponse `json:"data"`
	Errors []string               `json:"errors,omitempty"`
}

type decidedRequest struct {
	From    uint64          `json:"from"`
	To      uint64          `json:"to"`
	Roles   api.RoleSlice   `json:"roles"`
	PubKeys api.HexSlice    `json:"pubkeys"`
	Indices api.Uint64Slice `json:"indices"`
}

// implements filterRequest interface
func (r *decidedRequest) pubKeys() []spectypes.ValidatorPK {
	return parsePubkeysSlice(r.PubKeys)
}

// implements filterRequest interface
func (r *decidedRequest) indices() []uint64 {
	return r.Indices
}

// implements filterRequest interface
func (r *decidedRequest) hasFilters() bool {
	return len(r.PubKeys) > 0 || len(r.Indices) > 0
}

func toParticipantResponse(role spectypes.BeaconRole, entry qbftstorage.ParticipantsRangeEntry) *participantResponse {
	response := &participantResponse{
		Role:      role.String(),
		Slot:      uint64(entry.Slot),
		PublicKey: hex.EncodeToString(entry.PubKey[:]),
	}
	response.Message.Signers = entry.Signers

	return response
}
