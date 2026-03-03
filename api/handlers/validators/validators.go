package validators

import (
	"bytes"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
)

type Validators struct {
	Shares registrystorage.Shares
}

// List godoc
// @Summary Get validators
// @Description Returns the list of validators managed by the SSV node. Pagination is provided via the JSON request body under "pagination".
// @Tags Validators
// @Accept json
// @Produce json
// @Param request body ValidatorsRequest false "Filters and pagination as JSON body"
// @Success 200 {object} ValidatorsResponse
// @Failure 400 {object} api.ErrorResponse
// @Failure 429 {object} api.ErrorResponse "Too Many Requests"
// @Failure 500 {object} api.ErrorResponse
// @Router /v1/validators [get]
func (h *Validators) List(w http.ResponseWriter, r *http.Request) error {
	const (
		defaultPerPage = uint64(1000)
		maxPerPage     = uint64(10000)
	)

	if r.URL.Query().Has("page") || r.URL.Query().Has("per_page") || r.URL.Query().Has("pagination") {
		return api.BadRequestError(fmt.Errorf("pagination must be provided in request body"))
	}

	var request ValidatorsRequest
	if err := api.Bind(r, &request); err != nil {
		return api.BadRequestError(err)
	}

	pagination, err := request.Pagination.ToPagination(api.PaginationOptions{
		DefaultPerPage: defaultPerPage,
		MaxPerPage:     maxPerPage,
	})
	if err != nil {
		return api.BadRequestError(err)
	}
	paginationRequested := pagination != nil

	var filters []registrystorage.SharesFilter
	if len(request.Owners) > 0 {
		filters = append(filters, byOwners(request.Owners))
	}
	if len(request.Operators) > 0 {
		filters = append(filters, byOperators(request.Operators))
	}
	if len(request.Clusters) > 0 {
		filters = append(filters, byClusters(request.Clusters, false))
	}
	if len(request.Subclusters) > 0 {
		filters = append(filters, byClusters(request.Subclusters, true))
	}
	if len(request.PubKeys) > 0 {
		filters = append(filters, byPubKeys(request.PubKeys))
	}
	if len(request.Indices) > 0 {
		filters = append(filters, byIndices(request.Indices))
	}

	shares := h.Shares.List(nil, filters...)

	var response ValidatorsResponse

	// if no pagination requested, return retro-compatible response without pagination metadata
	if !paginationRequested {
		response.Data = make([]*Validator, len(shares))
		for i, share := range shares {
			response.Data[i] = validatorFromShare(share)
		}
		return api.Render(w, r, response)
	}

	// Ensure deterministic ordering for pagination.
	sort.Slice(shares, func(i, j int) bool {
		return bytes.Compare(shares[i].ValidatorPubKey[:], shares[j].ValidatorPubKey[:]) < 0
	})

	total := uint64(len(shares))
	start, end := pagination.SliceBounds(total)

	pagedShares := shares[start:end]
	response.Data = make([]*Validator, len(pagedShares))
	for i, share := range pagedShares {
		response.Data[i] = validatorFromShare(share)
	}

	p := api.PaginationResponseFromPagination(*pagination, total)
	response.Pagination = &p

	return api.Render(w, r, response)
}

func byOwners(owners []api.Hex) registrystorage.SharesFilter {
	return func(share *types.SSVShare) bool {
		for _, a := range owners {
			if bytes.Equal(a, share.OwnerAddress[:]) {
				return true
			}
		}
		return false
	}
}

func byOperators(operators []uint64) registrystorage.SharesFilter {
	return func(share *types.SSVShare) bool {
		for _, a := range operators {
			for _, b := range share.Committee {
				if a == b.Signer {
					return true
				}
			}
		}
		return false
	}
}

// byClusters returns a filter that matches shares that match or contain any of the given clusters.
func byClusters(clusters Clusters, contains bool) registrystorage.SharesFilter {
	return func(share *types.SSVShare) bool {
		shareCommittee := make([]string, len(share.Committee))
		for i, c := range share.Committee {
			shareCommittee[i] = strconv.FormatUint(c.Signer, 10)
		}
		shareStr := strings.Join(shareCommittee, ",")

		for _, cluster := range clusters {
			clusterStrs := make([]string, len(cluster))
			for i, c := range cluster {
				clusterStrs[i] = strconv.FormatUint(c, 10)
			}
			clusterStr := strings.Join(clusterStrs, ",")

			if contains && strings.Contains(shareStr, clusterStr) {
				return true
			}
			if !contains && shareStr == clusterStr {
				return true
			}
		}
		return false
	}
}

func byPubKeys(pubkeys []api.Hex) registrystorage.SharesFilter {
	return func(share *types.SSVShare) bool {
		for _, pubKey := range pubkeys {
			if bytes.Equal(pubKey, share.ValidatorPubKey[:]) {
				return true
			}
		}
		return false
	}
}

func byIndices(indices []uint64) registrystorage.SharesFilter {
	return func(share *types.SSVShare) bool {
		for _, index := range indices {
			if share.ValidatorIndex == phase0.ValidatorIndex(index) {
				return true
			}
		}
		return false
	}
}
