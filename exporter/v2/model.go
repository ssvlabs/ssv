package exporter

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

//go:generate sszgen -include ../../vendor/github.com/attestantio/go-eth2-client/spec/phase0,../../vendor/github.com/ssvlabs/ssv-spec/types --path model.go --objs ValidatorDutyTrace,CommitteeDutyTrace
type ValidatorDutyTrace struct {
	DutyTrace
	Pre       []*MessageTrace `ssz-max:"13"`
	Post      []*MessageTrace `ssz-max:"13"`
	Role      spectypes.BeaconRole
	Validator phase0.ValidatorIndex
}

type CommitteeDutyTrace struct {
	DutyTrace
	Post []*CommitteeMessageTrace `ssz-max:"13"`

	CommitteeID spectypes.CommitteeID  `ssz-size:"32"`
	OperatorIDs []spectypes.OperatorID `ssz-max:"13"`

	// maybe not needed
	AttestationDataRoot      phase0.Root `ssz-size:"32"`
	SyncCommitteeMessageRoot phase0.Root `ssz-size:"32"`
}

type CommitteeMessageTrace struct {
	BeaconRoot []phase0.Root           `ssz-max:"1500" ssz-size:"32"`
	Validators []phase0.ValidatorIndex `ssz-max:"1500"`

	Signer       spectypes.OperatorID
	ReceivedTime uint64 // TODO fixme
}

type DutyTrace struct {
	Slot   phase0.Slot
	Rounds []*RoundTrace `ssz-max:"15"`
}

type RoundTrace struct {
	Proposer spectypes.OperatorID // can be computed or saved
	// ProposalData
	ProposalRoot         phase0.Root         `ssz-size:"32"`
	ProposalReceivedTime uint64              // TODO fix time
	Prepares             []*MessageTrace     `ssz-max:"13"` // Only recorded if root matches proposal.
	Commits              []*MessageTrace     `ssz-max:"13"` // Only recorded if root matches proposal.
	RoundChanges         []*RoundChangeTrace `ssz-max:"13"`
}

type RoundChangeTrace struct {
	MessageTrace
	PreparedRound   uint8
	PrepareMessages []*MessageTrace `ssz-max:"13"`
}

type MessageTrace struct {
	Round        uint8
	BeaconRoot   phase0.Root `ssz-size:"32"`
	Signer       spectypes.OperatorID
	ReceivedTime uint64 // TODO fix time
}
