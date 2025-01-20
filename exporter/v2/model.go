package exporter

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

type ValidatorDutyTrace struct {
	DutyTrace
	Role      spectypes.BeaconRole
	Validator phase0.ValidatorIndex
}

type CommitteeDutyTrace struct {
	DutyTrace
	CommitteeID              spectypes.CommitteeID  `ssz-size:"32"`
	OperatorIDs              []spectypes.OperatorID `ssz-max:"4"`
	AttestationDataRoot      phase0.Root            `ssz-size:"32"` // Computed from BeaconVote: See example in CommitteeObserver
	SyncCommitteeMessageRoot phase0.Root            `ssz-size:"32"` // Computed from BeaconVote: See example in CommitteeObserver
}

type DutyTrace struct {
	Slot   phase0.Slot
	Pre    []*MessageTrace `ssz-max:"4"`
	Rounds []*RoundTrace   `ssz-max:"4"`
	Post   []*MessageTrace `ssz-max:"4"`
}

type RoundTrace struct {
	ProposalRoot     [32]byte        `ssz-size:"32"`
	ProposalReceived uint64          // TODO fixme
	Prepares         []*MessageTrace `ssz-max:"4"` // Only recorded if root matches proposal.
	Commits          []*MessageTrace `ssz-max:"4"` // Only recorded if root matches proposal.
}

type MessageTrace struct {
	BeaconRoot phase0.Root `ssz-size:"32"`
	Signer     spectypes.OperatorID
	Validator  phase0.ValidatorIndex // Only present in CommitteeDutyTrace.Post
	Received   uint64                // TODO fixme
}
