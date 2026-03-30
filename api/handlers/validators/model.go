package validators

import (
	"strconv"
	"strings"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

// Clusters represents clusters of operator IDs.
//
// Query format: space-separated list of comma-separated operator IDs (e.g. "1,2,3,4 5,6,7,8").
// JSON format: array of arrays (e.g. [[1,2,3,4],[5,6,7,8]]).
type Clusters [][]uint64

func (c *Clusters) Bind(value string) error {
	if value == "" {
		return nil
	}
	for s := range strings.SplitSeq(value, " ") {
		var cluster []uint64
		for s := range strings.SplitSeq(s, ",") {
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

// ValidatorsRequest represents the filters accepted by the validators endpoint.
type ValidatorsRequest struct {
	Owners      api.HexSlice          `json:"owners" form:"owners" swaggertype:"array,string" format:"hex"`
	Operators   api.Uint64Slice       `json:"operators" form:"operators" swaggertype:"array,integer" format:"int64" minimum:"0"`
	Clusters    Clusters              `json:"clusters" form:"clusters"`
	Subclusters Clusters              `json:"subclusters" form:"subclusters"`
	PubKeys     api.HexSlice          `json:"pubkeys" form:"pubkeys" swaggertype:"array,string" format:"hex"`
	Indices     api.Uint64Slice       `json:"indices" form:"indices" swaggertype:"array,integer" format:"int64" minimum:"0"`
	Pagination  api.PaginationRequest `json:"pagination" form:"pagination"`
}

// ValidatorsResponse represents the response from the validators endpoint.
type ValidatorsResponse struct {
	Data       []*Validator            `json:"data"`
	Pagination *api.PaginationResponse `json:"pagination,omitempty"`
}

type Validator struct {
	PubKey          api.Hex                `json:"public_key"`
	Index           phase0.ValidatorIndex  `json:"index" swaggertype:"string" example:"123"`
	Status          string                 `json:"status"`
	ActivationEpoch phase0.Epoch           `json:"activation_epoch" swaggertype:"string" example:"0"`
	ExitEpoch       phase0.Epoch           `json:"exit_epoch" swaggertype:"string" example:"0"`
	Owner           api.Hex                `json:"owner"`
	Committee       []spectypes.OperatorID `json:"committee" swaggertype:"array,integer" format:"int64" minimum:"0"`
	Quorum          uint64                 `json:"quorum"`
	PartialQuorum   uint64                 `json:"partial_quorum"`
	Graffiti        string                 `json:"graffiti"`
	Liquidated      bool                   `json:"liquidated"`
}

func validatorFromShare(share *types.SSVShare) *Validator {
	v := &Validator{
		PubKey: api.Hex(share.ValidatorPubKey[:]),
		Owner:  api.Hex(share.OwnerAddress[:]),
		Committee: func() []spectypes.OperatorID {
			committee := make([]spectypes.OperatorID, len(share.Committee))
			for i, op := range share.Committee {
				committee[i] = op.Signer
			}
			return committee
		}(),
		Graffiti:   string(share.Graffiti),
		Liquidated: share.Liquidated,
	}
	if share.HasBeaconMetadata() {
		v.Index = share.ValidatorIndex
		v.Status = share.Status.String()
		v.ActivationEpoch = share.ActivationEpoch
		v.ExitEpoch = share.ExitEpoch
	}
	return v
}
