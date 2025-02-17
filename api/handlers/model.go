package handlers

import (
	"encoding/hex"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	model "github.com/ssvlabs/ssv/exporter/v2"
)

type validatorTraceResponse struct {
	Data []validatorTrace `json:"data"`
}

type validatorTrace struct {
	Slot      phase0.Slot           `json:"slot"`
	Rounds    []round               `json:"consensus"`
	Decideds  []decided             `json:"decideds"`
	Pre       []message             `json:"pre"`
	Post      []message             `json:"post"`
	Role      string                `json:"role"`
	Validator phase0.ValidatorIndex `json:"validator"`
}

type decided struct {
	Round        uint64                 `json:"round"`
	BeaconRoot   phase0.Root            `json:"ssvRoot"`
	Signers      []spectypes.OperatorID `json:"signers"`
	ReceivedTime time.Time              `json:"time"`
}

type round struct {
	ProposalTrace *proposalTrace `json:"proposal"`
	// Proposer      spectypes.OperatorID `json:"proposer"` not needed
	Prepares     []message     `json:"prepares"`
	Commits      []message     `json:"commits"`
	RoundChanges []roundChange `json:"roundChanges"`
}

type proposalTrace struct {
	Round           uint64               `json:"round"`
	BeaconRoot      phase0.Root          `json:"ssvRoot"`
	Signer          spectypes.OperatorID `json:"signer"`
	RoundChanges    []roundChange        `json:"roundChanges"`
	PrepareMessages []message            `json:"prepareMessages"`
	ReceivedTime    time.Time            `json:"time"`
}

type roundChange struct {
	message
	PreparedRound   uint64    `json:"preparedRound"`
	PrepareMessages []message `json:"prepareMessages"`
}

type message struct {
	Round        uint64               `json:"round"`
	BeaconRoot   phase0.Root          `json:"ssvRoot"`
	Signer       spectypes.OperatorID `json:"signer"`
	ReceivedTime time.Time            `json:"time"`
}

func toValidatorTrace(t *model.ValidatorDutyTrace) validatorTrace {
	return validatorTrace{
		Slot:      t.Slot,
		Role:      t.Role.String(),
		Validator: t.Validator,
		Pre:       toMessageTrace(t.Pre),
		Post:      toMessageTrace(t.Post),
		Rounds:    toRounds(t.Rounds),
	}
}

func toMessageTrace(m []*model.PartialSigTrace) (out []message) {
	for _, mt := range m {
		out = append(out, message{
			BeaconRoot:   mt.BeaconRoot,
			Signer:       mt.Signer,
			ReceivedTime: mt.ReceivedTime,
		})
	}

	return
}

func toRounds(r []*model.RoundTrace) (out []round) {
	for _, rt := range r {
		out = append(out, round{
			// Proposer:      rt.Proposer,
			ProposalTrace: toProposalTrace(rt.ProposalTrace),
			Prepares:      toUIMessageTrace(rt.Prepares),
			Commits:       toUIMessageTrace(rt.Commits),
			RoundChanges:  toUIRoundChangeTrace(rt.RoundChanges),
		})
	}

	return
}

func toProposalTrace(rt *model.ProposalTrace) *proposalTrace {
	if rt == nil {
		return nil
	}
	return &proposalTrace{
		Round:           rt.Round,
		BeaconRoot:      rt.BeaconRoot,
		Signer:          rt.Signer,
		ReceivedTime:    rt.ReceivedTime,
		RoundChanges:    toUIRoundChangeTrace(rt.RoundChanges),
		PrepareMessages: toUIMessageTrace(rt.PrepareMessages),
	}
}

func toUIMessageTrace(m []*model.QBFTTrace) (out []message) {
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

func toUIRoundChangeTrace(m []*model.RoundChangeTrace) (out []roundChange) {
	for _, mt := range m {
		out = append(out, roundChange{
			message: message{
				Round:        mt.Round,
				BeaconRoot:   mt.BeaconRoot,
				Signer:       mt.Signer,
				ReceivedTime: mt.ReceivedTime,
			},
			PreparedRound:   mt.PreparedRound,
			PrepareMessages: toUIMessageTrace(mt.PrepareMessages),
		})
	}

	return
}

// committee

type committeeTraceResponse struct {
	Data []committeeTrace `json:"data"`
}

type committeeTrace struct {
	Slot     phase0.Slot `json:"slot"`
	Rounds   []round     `json:"consensus"`
	Decideds []decided   `json:"decideds"`

	SyncCommittee []committeeMessage `json:"sync_committee"`
	Attester      []committeeMessage `json:"attester"`

	CommitteeID string `json:"committeeID"`
	// OperatorIDs []spectypes.OperatorID `json:"operatorIDs"` not needed?
}

type committeeMessage struct {
	Signers      []spectypes.OperatorID `json:"signers"`
	ReceivedTime time.Time              `json:"time"`
}

func toCommitteeTrace(t *model.CommitteeDutyTrace) committeeTrace {
	return committeeTrace{
		// consensus trace
		Slot:          t.Slot,
		Rounds:        toRounds(t.Rounds),
		Decideds:      toDecidedTrace(t.Decideds),
		SyncCommittee: toCommitteePost(t.SyncCommittee),
		Attester:      toCommitteePost(t.Attester),
		CommitteeID:   hex.EncodeToString(t.CommitteeID[:]),
		// OperatorIDs: t.OperatorIDs,
	}
}

func toDecidedTrace(d []*model.DecidedTrace) (out []decided) {
	for _, dt := range d {
		out = append(out, decided{
			Round:        dt.Round,
			BeaconRoot:   dt.BeaconRoot,
			Signers:      dt.Signers,
			ReceivedTime: dt.ReceivedTime,
		})
	}

	return
}

func toCommitteePost(m []*model.SignerData) (out []committeeMessage) {
	for _, mt := range m {
		out = append(out, committeeMessage{
			Signers:      mt.Signers,
			ReceivedTime: mt.ReceivedTime,
		})
	}

	return
}
