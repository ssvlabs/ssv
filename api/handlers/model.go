package handlers

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/api"
	model "github.com/ssvlabs/ssv/exporter"
)

// === Decideds ======================================================================
type ParticipantResponse struct {
	Role      string `json:"role"`
	Slot      uint64 `json:"slot"`
	PublicKey string `json:"public_key"`
	Message   struct {
		// We're keeping "Signers" capitalized to avoid breaking existing clients that rely on the current structure
		Signers []uint64 `json:"Signers"`
	} `json:"message"`
}

type decidedResponse struct {
	Data   []*ParticipantResponse `json:"data"`
	Errors []string               `json:"errors,omitempty"`
}

type decidedRequest struct {
	From    uint64        `json:"from"`
	To      uint64        `json:"to"`
	Roles   api.RoleSlice `json:"roles"`
	PubKeys api.HexSlice  `json:"pubkeys"`
}

func (r *decidedRequest) parsePubkeys() []spectypes.ValidatorPK {
	pubKeys := make([]spectypes.ValidatorPK, len(r.PubKeys))
	for i, pk := range r.PubKeys {
		copy(pubKeys[i][:], pk)
	}
	return pubKeys
}

// === ValidatorTrace ======================================================================
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

type decided struct {
	Round        uint64                 `json:"round"`
	BeaconRoot   phase0.Root            `json:"ssvRoot"`
	Signers      []spectypes.OperatorID `json:"signers"`
	ReceivedTime time.Time              `json:"time"`
}

type round struct {
	Proposal     *proposalTrace `json:"proposal"`
	Prepares     []message      `json:"prepares"`
	Commits      []message      `json:"commits"`
	RoundChanges []roundChange  `json:"roundChanges"`
}

type proposalTrace struct {
	Round                 uint64               `json:"round"`
	BeaconRoot            phase0.Root          `json:"ssvRoot"`
	Signer                spectypes.OperatorID `json:"leader"`
	RoundChanges          []roundChange        `json:"roundChangeJustifications"`
	PrepareJustifications []message            `json:"prepareJustifications"`
	ReceivedTime          time.Time            `json:"time"`
}

type roundChange struct {
	message
	PreparedRound   uint64    `json:"preparedRound"`
	PrepareMessages []message `json:"prepareMessages"`
}

type message struct {
	Round        uint64               `json:"round,omitempty"`
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
		Proposal:  fmt.Sprintf("%x", t.ProposalData),
		Decideds:  toDecideds(t.Decideds),
	}
}

func toMessageTrace(m []*model.PartialSigTrace) (out []message) {
	for _, mt := range m {
		out = append(out, message{
			BeaconRoot:   mt.BeaconRoot,
			Signer:       mt.Signer,
			ReceivedTime: toTime(mt.ReceivedTime),
		})
	}

	return
}

func toRounds(r []*model.RoundTrace) (out []round) {
	for _, rt := range r {
		out = append(out, round{
			Proposal:     toProposalTrace(rt.ProposalTrace),
			Prepares:     toUIMessageTrace(rt.Prepares),
			Commits:      toUIMessageTrace(rt.Commits),
			RoundChanges: toUIRoundChangeTrace(rt.RoundChanges),
		})
	}

	return
}

func toProposalTrace(rt *model.ProposalTrace) *proposalTrace {
	if rt == nil {
		return nil
	}

	// Filter out zero-valued ProposalTrace objects created by SSZ encoding.
	// These occur when rounds exist with only roundChanges (or possibly also prepares/commits) but no actual proposal.
	// Without filtering, they appear as confusing proposals with round=0, leader=0, epoch timestamp.
	// While 0-valued proposals are technically valid according to our data model, they show up as confusing API responses.
	if rt.Round == 0 && rt.Signer == 0 && rt.ReceivedTime == 0 {
		return nil
	}

	return &proposalTrace{
		Round:                 rt.Round,
		BeaconRoot:            rt.BeaconRoot,
		Signer:                rt.Signer,
		ReceivedTime:          toTime(rt.ReceivedTime),
		RoundChanges:          toUIRoundChangeTrace(rt.RoundChanges),
		PrepareJustifications: toUIMessageTrace(rt.PrepareMessages),
	}
}

func toUIMessageTrace(m []*model.QBFTTrace) (out []message) {
	for _, mt := range m {
		out = append(out, message{
			Round:        mt.Round,
			BeaconRoot:   mt.BeaconRoot,
			Signer:       mt.Signer,
			ReceivedTime: toTime(mt.ReceivedTime),
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
				ReceivedTime: toTime(mt.ReceivedTime),
			},
			PreparedRound:   mt.PreparedRound,
			PrepareMessages: toUIMessageTrace(mt.PrepareMessages),
		})
	}

	return
}

// === CommiteeTrace ======================================================================

type committeeRequest struct {
	From         uint64       `json:"from"`
	To           uint64       `json:"to"`
	CommitteeIDs api.HexSlice `json:"committeeIDs"`
}

func (req *committeeRequest) parseCommitteeIds() []spectypes.CommitteeID {
	committeeIDs := make([]spectypes.CommitteeID, len(req.CommitteeIDs))
	for i, cmt := range req.CommitteeIDs {
		copy(committeeIDs[i][:], cmt)
	}
	return committeeIDs
}

type committeeTraceResponse struct {
	Data   []committeeTrace `json:"data"`
	Errors []string         `json:"errors,omitempty"`
}

type committeeTrace struct {
	Slot      uint64    `json:"slot"`
	Consensus []round   `json:"consensus"`
	Decideds  []decided `json:"decideds"`

	SyncCommittee []committeeMessage `json:"sync_committee"`
	Attester      []committeeMessage `json:"attester"`

	CommitteeID string `json:"committeeID"`
	Proposal    string `json:"proposalData,omitempty"`
}

type committeeMessage struct {
	Signer       uint64    `json:"signer"`
	ValidatorIdx []uint64  `json:"validatorIdx"`
	ReceivedTime time.Time `json:"time"`
}

func toCommitteeTrace(t *model.CommitteeDutyTrace) committeeTrace {
	return committeeTrace{
		// consensus trace
		Slot:          uint64(t.Slot),
		Consensus:     toRounds(t.Rounds),
		Decideds:      toDecideds(t.Decideds),
		SyncCommittee: toCommitteePost(t.SyncCommittee),
		Attester:      toCommitteePost(t.Attester),
		CommitteeID:   hex.EncodeToString(t.CommitteeID[:]),
		Proposal:      fmt.Sprintf("%x", t.ProposalData),
	}
}

func toDecideds(d []*model.DecidedTrace) (out []decided) {
	for _, dt := range d {
		out = append(out, decided{
			Round:        dt.Round,
			BeaconRoot:   dt.BeaconRoot,
			Signers:      dt.Signers,
			ReceivedTime: toTime(dt.ReceivedTime),
		})
	}

	return
}

func toCommitteePost(m []*model.SignerData) (out []committeeMessage) {
	for _, mt := range m {
		out = append(out, committeeMessage{
			Signer:       mt.Signer,
			ValidatorIdx: toUint64Slice(mt.ValidatorIdx),
			ReceivedTime: toTime(mt.ReceivedTime),
		})
	}

	return
}

// === Helpers ======================================================================

func toUint64Slice(s []phase0.ValidatorIndex) []uint64 {
	out := make([]uint64, len(s))
	for i, v := range s {
		out[i] = uint64(v)
	}

	return out
}

//nolint:gosec
func toTime(t uint64) time.Time {
	return time.UnixMilli(int64(t))
}
