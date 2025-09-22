package exporter

import (
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter"
)

type filterRequest interface {
	pubKeys() []spectypes.ValidatorPK
	indices() []uint64
	hasFilters() bool
}

// Decided represents a decided message within a duty trace.
type Decided struct {
	// Round is the QBFT round number when the decision was made.
	Round uint64 `json:"round" format:"int64" example:"2"`
	// BeaconRoot is the decided root value (hex-encoded).
	BeaconRoot phase0.Root `json:"ssvRoot" swaggertype:"string" format:"hex" example:"f3a1..."`
	// Signers lists operator IDs that signed the decided message.
	Signers []spectypes.OperatorID `json:"signers" swaggertype:"array,integer"`
	// ReceivedTime is when the decided message was observed.
	ReceivedTime time.Time `json:"time" format:"date-time"`
}

// Round captures the per-round consensus trace details.
type Round struct {
	// Proposal is the proposal observed in this round, if any.
	Proposal *ProposalTrace `json:"proposal"`
	// Prepares lists prepare messages received in this round.
	Prepares []Message `json:"prepares"`
	// Commits lists commit messages received in this round.
	Commits []Message `json:"commits"`
	// RoundChanges lists round change justifications that led to this round.
	RoundChanges []RoundChange `json:"roundChanges"`
}

// ProposalTrace stores proposal data for a consensus round.
type ProposalTrace struct {
	// Round is the round number for this proposal.
	Round uint64 `json:"round" format:"int64" example:"1"`
	// BeaconRoot is the proposed root value (hex-encoded).
	BeaconRoot phase0.Root `json:"ssvRoot" swaggertype:"string" format:"hex"`
	// Signer is the leader operator ID that proposed.
	Signer spectypes.OperatorID `json:"leader" swaggertype:"integer"`
	// RoundChanges holds the round-change justifications included in the proposal.
	RoundChanges []RoundChange `json:"roundChangeJustifications"`
	// PrepareJustifications holds the prepare justifications included in the proposal.
	PrepareJustifications []Message `json:"prepareJustifications"`
	// ReceivedTime is when the proposal was observed.
	ReceivedTime time.Time `json:"time" format:"date-time"`
}

// RoundChange provides justification information for a round change event.
type RoundChange struct {
	Message
	// PreparedRound is the highest prepared round the sender claims.
	PreparedRound uint64 `json:"preparedRound" format:"int64"`
	// PrepareMessages lists the prepare messages that justify the prepared round.
	PrepareMessages []Message `json:"prepareMessages"`
}

// Message represents a QBFT message trace entry.
type Message struct {
	// Round is the round associated with this message.
	Round uint64 `json:"round,omitempty" format:"int64"`
	// BeaconRoot is the message root value (hex-encoded).
	BeaconRoot phase0.Root `json:"ssvRoot" swaggertype:"string" format:"hex"`
	// Signer is the operator ID that sent the message.
	Signer spectypes.OperatorID `json:"signer" swaggertype:"integer"`
	// ReceivedTime is when the message was observed.
	ReceivedTime time.Time `json:"time" format:"date-time"`
}

func toDecideds(d []*exporter.DecidedTrace) (out []Decided) {
	for _, dt := range d {
		out = append(out, Decided{
			Round:        dt.Round,
			BeaconRoot:   dt.BeaconRoot,
			Signers:      dt.Signers,
			ReceivedTime: toTime(dt.ReceivedTime),
		})
	}
	return
}

func toRounds(r []*exporter.RoundTrace) (out []Round) {
	for _, rt := range r {
		out = append(out, Round{
			Proposal:     toProposalTrace(rt.ProposalTrace),
			Prepares:     toUIMessageTrace(rt.Prepares),
			Commits:      toUIMessageTrace(rt.Commits),
			RoundChanges: toUIRoundChangeTrace(rt.RoundChanges),
		})
	}
	return
}

func toProposalTrace(rt *exporter.ProposalTrace) *ProposalTrace {
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

	return &ProposalTrace{
		Round:                 rt.Round,
		BeaconRoot:            rt.BeaconRoot,
		Signer:                rt.Signer,
		ReceivedTime:          toTime(rt.ReceivedTime),
		RoundChanges:          toUIRoundChangeTrace(rt.RoundChanges),
		PrepareJustifications: toUIMessageTrace(rt.PrepareMessages),
	}
}

func toUIMessageTrace(m []*exporter.QBFTTrace) (out []Message) {
	for _, mt := range m {
		out = append(out, Message{
			Round:        mt.Round,
			BeaconRoot:   mt.BeaconRoot,
			Signer:       mt.Signer,
			ReceivedTime: toTime(mt.ReceivedTime),
		})
	}
	return
}

func toUIRoundChangeTrace(m []*exporter.RoundChangeTrace) (out []RoundChange) {
	for _, mt := range m {
		out = append(out, RoundChange{
			Message: Message{
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

//nolint:gosec
func toTime(t uint64) time.Time { return time.UnixMilli(int64(t)) }

// toMessageTrace converts PartialSigTrace to API message trace model
func toMessageTrace(m []*exporter.PartialSigTrace) (out []Message) {
	for _, mt := range m {
		out = append(out, Message{
			BeaconRoot:   mt.BeaconRoot,
			Signer:       mt.Signer,
			ReceivedTime: toTime(mt.ReceivedTime),
		})
	}
	return
}

func toUint64Slice(s []phase0.ValidatorIndex) []uint64 {
	out := make([]uint64, len(s))
	for i, v := range s {
		out[i] = uint64(v)
	}
	return out
}

// Formatting helper to avoid fmt import in validator model
func formatProposalData(b []byte) string { return fmt.Sprintf("%x", b) }
