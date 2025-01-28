package handlers

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	model "github.com/ssvlabs/ssv/exporter/v2"
)

type validatorTraceResponse struct {
	Data []validatorTrace `json:"data"`
}

type validatorTrace struct {
	Slot      phase0.Slot `json:"slot"`
	Rounds    []round
	Pre       []message             `json:"pre"`
	Post      []message             `json:"post"`
	Role      spectypes.BeaconRole  `json:"role"`
	Validator phase0.ValidatorIndex `json:"validator"`
}

type round struct {
	Proposer             spectypes.OperatorID `json:"proposer"`
	ProposalRoot         phase0.Root          `json:"proposalRoot"`
	ProposalReceivedTime uint64               `json:"time"`
	Prepares             []message            `json:"prepares"`
	Commits              []message            `json:"commits"`
	RoundChanges         []roundChange        `json:"roundChanges"`
}

type roundChange struct {
	message
	PreparedRound   uint8     `json:"preparedRound"`
	PrepareMessages []message `json:"prepareMessages"`
}

type message struct {
	Round        uint8                `json:"round"`
	BeaconRoot   phase0.Root          `json:"beaconRoot"`
	Signer       spectypes.OperatorID `json:"signer"`
	ReceivedTime uint64               `json:"time"`
}

func toValidatorTrace(t *model.ValidatorDutyTrace) validatorTrace {
	return validatorTrace{
		Slot:      t.Slot,
		Role:      t.Role,
		Validator: t.Validator,
		Pre:       toMessageTrace(t.Pre),
		Post:      toMessageTrace(t.Post),
		Rounds:    toRoundTrace(t.Rounds),
	}
}

func toMessageTrace(m []*model.MessageTrace) (out []message) {
	for _, mt := range m {
		out = append(out, message{
			Round:        mt.Round,
			BeaconRoot:   mt.BeaconRoot,
			Signer:       mt.Signer,
			ReceivedTime: mt.ReceivedTime,
		})
	}
	return
}

func toRoundTrace(r []*model.RoundTrace) (out []round) {
	for _, rt := range r {
		out = append(out, round{
			Proposer:             rt.Proposer,
			ProposalRoot:         rt.ProposalRoot,
			ProposalReceivedTime: rt.ProposalReceivedTime,
		})
	}
	return
}

// committee

type committeeTraceResponse struct {
	Data []committeeTrace `json:"data"`
}

type committeeTrace struct {
	Slot   phase0.Slot        `json:"slot"`
	Rounds []round            `json:"rounds"`
	Post   []committeeMessage `json:"post"`

	CommitteeID spectypes.CommitteeID  `json:"committeeID"`
	OperatorIDs []spectypes.OperatorID `json:"operatorIDs"`
}

type committeeMessage struct {
	BeaconRoot   []phase0.Root           `json:"beaconRoot"`
	Validators   []phase0.ValidatorIndex `json:"validators"`
	Signer       spectypes.OperatorID    `json:"signer"`
	ReceivedTime uint64                  `json:"time"`
}

func toCommitteeTrace(t *model.CommitteeDutyTrace) committeeTrace {
	return committeeTrace{
		Slot:        t.Slot,
		Rounds:      toRoundTrace(t.Rounds),
		Post:        toCommitteeMessageTrace(t.Post),
		CommitteeID: t.CommitteeID,
		OperatorIDs: t.OperatorIDs,
	}
}

func toCommitteeMessageTrace(m []*model.CommitteeMessageTrace) (out []committeeMessage) {
	for _, mt := range m {
		out = append(out, committeeMessage{
			BeaconRoot:   mt.BeaconRoot,
			Validators:   mt.Validators,
			Signer:       mt.Signer,
			ReceivedTime: mt.ReceivedTime,
		})
	}
	return
}
