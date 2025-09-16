package exporter

import (
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter"
)

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

func toDecideds(d []*exporter.DecidedTrace) (out []decided) {
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

func toRounds(r []*exporter.RoundTrace) (out []round) {
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

func toProposalTrace(rt *exporter.ProposalTrace) *proposalTrace {
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

func toUIMessageTrace(m []*exporter.QBFTTrace) (out []message) {
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

func toUIRoundChangeTrace(m []*exporter.RoundChangeTrace) (out []roundChange) {
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

//nolint:gosec
func toTime(t uint64) time.Time { return time.UnixMilli(int64(t)) }

// toMessageTrace converts PartialSigTrace to API message trace model
func toMessageTrace(m []*exporter.PartialSigTrace) (out []message) {
	for _, mt := range m {
		out = append(out, message{
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
