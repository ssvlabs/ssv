package validators

import (
	"bytes"
	"net/http"
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

func (h *Validators) List(w http.ResponseWriter, r *http.Request) error {
	var request struct {
		Owners      api.HexSlice    `json:"owners" form:"owners"`
		Operators   api.Uint64Slice `json:"operators" form:"operators"`
		Clusters    requestClusters `json:"clusters" form:"clusters"`
		Subclusters requestClusters `json:"subclusters" form:"subclusters"`
		PubKeys     api.HexSlice    `json:"pubkeys" form:"pubkeys"`
		Indices     api.Uint64Slice `json:"indices" form:"indices"`
	}
	var response struct {
		Data []*validatorJSON `json:"data"`
	}

	if err := api.Bind(r, &request); err != nil {
		return err
	}

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
	response.Data = make([]*validatorJSON, len(shares))
	for i, share := range shares {
		response.Data[i] = validatorFromShare(share)
	}
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
func byClusters(clusters requestClusters, contains bool) registrystorage.SharesFilter {
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
