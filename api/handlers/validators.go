package handlers

import (
	"bytes"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/api"
	"github.com/bloxapp/ssv/protocol/v2/types"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
)

type Validators struct {
	Shares registrystorage.Shares
}

func (h *Validators) List(w http.ResponseWriter, r *http.Request) error {
	var request struct {
		Operators api.Uint64Slice `json:"operators" form:"operators"`
		Clusters  requestClusters `json:"clusters" form:"clusters"`
		PubKeys   api.HexSlice    `json:"pubkeys" form:"pubkeys"`
		Indices   api.Uint64Slice `json:"indices" form:"indices"`
	}
	var response struct {
		Data []*validatorJSON `json:"data"`
	}

	if err := api.Bind(r, &request); err != nil {
		return err
	}

	var filters []registrystorage.SharesFilter
	if len(request.Operators) > 0 {
		filters = append(filters, byOperators(request.Operators))
	}
	if len(request.Clusters) > 0 {
		filters = append(filters, byClusters(request.Clusters))
	}
	if len(request.PubKeys) > 0 {
		filters = append(filters, byPubKeys(request.PubKeys))
	}
	if len(request.Indices) > 0 {
		filters = append(filters, byIndices(request.Indices))
	}

	shares := h.Shares.List(filters...)
	response.Data = make([]*validatorJSON, len(shares))
	for i, share := range shares {
		response.Data[i] = validatorFromShare(share)
	}
	return api.Render(w, r, response)
}

func byOperators(operators []uint64) registrystorage.SharesFilter {
	return func(share *types.SSVShare) bool {
		for _, a := range operators {
			for _, b := range share.Committee {
				if a == b.OperatorID {
					return true
				}
			}
		}
		return false
	}
}

func byClusters(clusters requestClusters) registrystorage.SharesFilter {
	return func(share *types.SSVShare) bool {
	Filter:
		for _, cluster := range clusters {
			if len(cluster) != len(share.Committee) {
				continue
			}
			sort.Slice(cluster, func(i, j int) bool { return cluster[i] < cluster[j] })
			for i, c := range share.Committee {
				if cluster[i] != c.OperatorID {
					continue Filter
				}
			}
			return true
		}
		return false
	}
}

func byPubKeys(pubkeys []api.Hex) registrystorage.SharesFilter {
	return func(share *types.SSVShare) bool {
		for _, pubKey := range pubkeys {
			if bytes.Equal(pubKey, share.ValidatorPubKey) {
				return true
			}
		}
		return false
	}
}

func byIndices(indices []uint64) registrystorage.SharesFilter {
	return func(share *types.SSVShare) bool {
		for _, index := range indices {
			if share.Metadata.BeaconMetadata.Index == phase0.ValidatorIndex(index) {
				return true
			}
		}
		return false
	}
}

// requestClusters is a space-separated list of comma-separated lists of operator IDs.
type requestClusters [][]uint64

func (c *requestClusters) Bind(value string) error {
	if value == "" {
		return nil
	}
	for _, s := range strings.Split(value, " ") {
		var cluster []uint64
		for _, s := range strings.Split(s, ",") {
			n, err := strconv.ParseUint(s, 10, 64)
			if err != nil {
				return err
			}
			cluster = append(cluster, n)
		}
		*c = append(*c, cluster)
	}
	return nil
}

type validatorJSON struct {
	PubKey        api.Hex                `json:"public_key"`
	Committee     []spectypes.OperatorID `json:"committee"`
	Quorum        uint64                 `json:"quorum"`
	PartialQuorum uint64                 `json:"partial_quorum"`
	Grafitti      string                 `json:"grafitti"`
	Liquidated    bool                   `json:"liquidated"`
}

func validatorFromShare(share *types.SSVShare) *validatorJSON {
	v := &validatorJSON{
		PubKey: api.Hex(share.ValidatorPubKey),
		Committee: func() []spectypes.OperatorID {
			committee := make([]spectypes.OperatorID, len(share.Committee))
			for i, op := range share.Committee {
				committee[i] = op.OperatorID
			}
			return committee
		}(),
		Quorum:        share.Quorum,
		PartialQuorum: share.PartialQuorum,
		Grafitti:      string(share.Graffiti),
		Liquidated:    share.Liquidated,
	}
	return v
}
