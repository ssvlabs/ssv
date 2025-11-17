package validators

import (
	"strconv"
	"strings"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

// requestClusters is a space-separated list of comma-separated lists of operator IDs.
type requestClusters [][]uint64

func (c *requestClusters) Bind(value string) error {
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

type validatorJSON struct {
	PubKey          api.Hex                `json:"public_key"`
	Index           phase0.ValidatorIndex  `json:"index"`
	Status          string                 `json:"status"`
	ActivationEpoch phase0.Epoch           `json:"activation_epoch"`
	ExitEpoch       phase0.Epoch           `json:"exit_epoch"`
	Owner           api.Hex                `json:"owner"`
	Committee       []spectypes.OperatorID `json:"committee"`
	Quorum          uint64                 `json:"quorum"`
	PartialQuorum   uint64                 `json:"partial_quorum"`
	Graffiti        string                 `json:"graffiti"`
	Liquidated      bool                   `json:"liquidated"`
}

func validatorFromShare(share *types.SSVShare) *validatorJSON {
	v := &validatorJSON{
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
