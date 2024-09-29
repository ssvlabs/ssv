package handlers

import (
	"fmt"
	"net/http"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/api"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
)

type Operators struct {
	Operators registrystorage.OperatorsReader
}

func (h *Operators) List(w http.ResponseWriter, r *http.Request) error {
	var request struct {
		IDs    api.Uint64Slice `json:"ids" form:"ids"`
		Owners api.HexSlice    `json:"owners" form:"owners"`
	}
	var response struct {
		Data []*operatorJSON `json:"data"`
	}

	if err := api.Bind(r, &request); err != nil {
		return err
	}

	// Optimize query for fast lookup.
	requestIDs := map[spectypes.OperatorID]struct{}{}
	for _, id := range request.IDs {
		requestIDs[spectypes.OperatorID(id)] = struct{}{}
	}
	requestOwners := map[string]struct{}{}
	for _, owner := range request.Owners {
		requestOwners[string(owner)] = struct{}{}
	}

	operators, err := h.Operators.ListOperators(nil, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to list operators: %w", err)
	}
	response.Data = make([]*operatorJSON, 0, len(operators))
	for _, operator := range operators {
		if len(request.IDs) > 0 {
			if _, ok := requestIDs[operator.ID]; !ok {
				continue
			}
		}
		if len(request.Owners) > 0 {
			if _, ok := requestOwners[string(operator.OwnerAddress.Bytes())]; !ok {
				continue
			}
		}
		response.Data = append(response.Data, &operatorJSON{
			ID:           operator.ID,
			PublicKey:    string(operator.PublicKey),
			OwnerAddress: api.Hex(operator.OwnerAddress.Bytes()),
		})
	}
	return api.Render(w, r, response)
}

type operatorJSON struct {
	ID           spectypes.OperatorID `json:"id"`
	PublicKey    string               `json:"public_key"`
	OwnerAddress api.Hex              `json:"owner_address"`
}
