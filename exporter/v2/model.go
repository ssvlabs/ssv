package exporter

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

//go:generate sszgen -include ../../vendor/github.com/attestantio/go-eth2-client/spec/phase0,../../vendor/github.com/ssvlabs/ssv-spec/types --path model.go --objs ValidatorDutyTrace,CommitteeDutyTrace
type ValidatorDutyTrace struct {
	ConsensusTrace

	Slot phase0.Slot
	Pre  []*PartialSigMessageTrace `ssz-max:"13"`
	Post []*PartialSigMessageTrace `ssz-max:"13"`
	Role spectypes.BeaconRole
	// this could be a pubkey
	Validator phase0.ValidatorIndex
}

type ConsensusTrace struct {
	Rounds   []*RoundTrace   `ssz-max:"15"`
	Decideds []*DecidedTrace `ssz-max:"256"` // TODO max
}

type DecidedTrace struct {
	MessageTrace
	// Value []byte // full data needed?
	Signers []spectypes.OperatorID `ssz-max:"13"`
}

type RoundTrace struct {
	Proposer spectypes.OperatorID // can be computed or saved
	// ProposalData
	ProposalTrace *ProposalTrace
	Prepares      []*MessageTrace     `ssz-max:"13"` // Only recorded if root matches proposal.
	Commits       []*MessageTrace     `ssz-max:"13"` // Only recorded if root matches proposal.
	RoundChanges  []*RoundChangeTrace `ssz-max:"13"`
}

type RoundChangeTrace struct {
	MessageTrace
	PreparedRound   uint64
	PrepareMessages []*MessageTrace `ssz-max:"13"`
}

type ProposalTrace struct {
	MessageTrace
	RoundChanges    []*RoundChangeTrace `ssz-max:"13"`
	PrepareMessages []*MessageTrace     `ssz-max:"13"`
}

type MessageTrace struct {
	Round        uint64      // same for
	BeaconRoot   phase0.Root `ssz-size:"32"`
	Signer       spectypes.OperatorID
	ReceivedTime time.Time
}

type PartialSigMessageTrace struct {
	Type         spectypes.PartialSigMsgType
	BeaconRoot   phase0.Root `ssz-size:"32"`
	Signer       spectypes.OperatorID
	ReceivedTime time.Time
}

// Committee
type CommitteeDutyTrace struct {
	ConsensusTrace

	Slot phase0.Slot
	Post []*CommitteePartialSigMessageTrace `ssz-max:"13"`

	CommitteeID spectypes.CommitteeID  `ssz-size:"32"`
	OperatorIDs []spectypes.OperatorID `ssz-max:"13"`

	// maybe not needed
	AttestationDataRoot      phase0.Root `ssz-size:"32"`
	SyncCommitteeMessageRoot phase0.Root `ssz-size:"32"`
}

type CommitteePartialSigMessageTrace struct {
	Type         spectypes.PartialSigMsgType
	Signer       spectypes.OperatorID
	Messages     []*PartialSigMessage `ssz-max:"1512"`
	ReceivedTime time.Time
}

type PartialSigMessage struct {
	BeaconRoot     phase0.Root `ssz-size:"32"`
	Signer         spectypes.OperatorID
	ValidatorIndex phase0.ValidatorIndex
}
