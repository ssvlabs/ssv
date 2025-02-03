package handlers

import (
	"time"

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
	Decideds  []decided
	Pre       []message             `json:"pre"`
	Post      []message             `json:"post"`
	Role      spectypes.BeaconRole  `json:"role"`
	Validator phase0.ValidatorIndex `json:"validator"`
}

type decided struct {
	Round        uint64
	BeaconRoot   phase0.Root
	Signer       spectypes.OperatorID
	ReceivedTime time.Time
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
	PreparedRound   uint64    `json:"preparedRound"`
	PrepareMessages []message `json:"prepareMessages"`
}

type message struct {
	Type         spectypes.PartialSigMsgType `json:"type"`
	BeaconRoot   phase0.Root                 `json:"beaconRoot"`
	Signer       spectypes.OperatorID        `json:"signer"`
	ReceivedTime time.Time                   `json:"time"`
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

func toMessageTrace(m []*model.PartialSigMessageTrace) (out []message) {
	for _, mt := range m {
		out = append(out, message{
			Type:         mt.Type,
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
			Proposer: rt.Proposer,
		})
	}
	return
}

// committee

type committeeTraceResponse struct {
	Data []committeeTrace `json:"data"`
}

type committeeTrace struct {
	Slot     phase0.Slot        `json:"slot"`
	Rounds   []round            `json:"rounds"`
	Decideds []decided          `json:"decideds"`
	Post     []committeeMessage `json:"post"`

	CommitteeID spectypes.CommitteeID  `json:"committeeID"`
	OperatorIDs []spectypes.OperatorID `json:"operatorIDs"`
}

type committeeMessage struct {
	message
	BeaconRoot   []phase0.Root           `json:"beaconRoot"`
	Validators   []phase0.ValidatorIndex `json:"validators"`
	Signer       spectypes.OperatorID    `json:"signer"`
	ReceivedTime time.Time               `json:"time"`
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

func toCommitteeMessageTrace(m []*model.CommitteePartialSigMessageTrace) (out []committeeMessage) {
	for _, mt := range m {
		out = append(out, committeeMessage{
			// Type:         mt.Type,
			// BeaconRoot:   mt.BeaconRoot,
			// Validators:   mt.Validators,
			Signer:       mt.Signer,
			ReceivedTime: mt.ReceivedTime,
		})
	}
	return
}
