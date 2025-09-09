package exporter

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/exporter"
)

type validatorRequest struct {
	From    uint64          `json:"from"`
	To      uint64          `json:"to"`
	Roles   api.RoleSlice   `json:"roles"`
	PubKeys api.HexSlice    `json:"pubkeys"`
	Indices api.Uint64Slice `json:"indices"`
}

type validatorTraceResponse struct {
	Data   []validatorTrace `json:"data"`
	Errors []string         `json:"errors,omitempty"`
}

type validatorTrace struct {
	Slot        phase0.Slot           `json:"slot"`
	Role        string                `json:"role"`
	Validator   phase0.ValidatorIndex `json:"validator"`
	CommitteeID string                `json:"committeeID,omitempty"`
	Rounds      []round               `json:"consensus"`
	Decideds    []decided             `json:"decideds"`
	Pre         []message             `json:"pre"`
	Post        []message             `json:"post"`
	Proposal    string                `json:"proposalData,omitempty"`
}

func toValidatorTrace(t *exporter.ValidatorDutyTrace) validatorTrace {
	return validatorTrace{
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
