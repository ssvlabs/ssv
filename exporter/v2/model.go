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
	Pre  []*PartialSigTrace `ssz-max:"13"`
	Post []*PartialSigTrace `ssz-max:"13"`
	Role spectypes.BeaconRole
	// this could be a pubkey
	Validator phase0.ValidatorIndex
}

type ConsensusTrace struct {
	Rounds   []*RoundTrace   `ssz-max:"15"`
	Decideds []*DecidedTrace `ssz-max:"256"` // TODO max
}

type DecidedTrace struct {
	Round        uint64                 // same for
	BeaconRoot   phase0.Root            `ssz-size:"32"`
	Signers      []spectypes.OperatorID `ssz-max:"13"`
	ReceivedTime time.Time
}

type RoundTrace struct {
	Proposer spectypes.OperatorID // can be computed or saved
	// ProposalData
	ProposalTrace *ProposalTrace
	Prepares      []*QBFTTrace        `ssz-max:"13"` // Only recorded if root matches proposal.
	Commits       []*QBFTTrace        `ssz-max:"13"` // Only recorded if root matches proposal.
	RoundChanges  []*RoundChangeTrace `ssz-max:"13"`
}

type RoundChangeTrace struct {
	QBFTTrace
	PreparedRound   uint64
	PrepareMessages []*QBFTTrace `ssz-max:"13"`
}

type ProposalTrace struct {
	QBFTTrace
	RoundChanges    []*RoundChangeTrace `ssz-max:"13"`
	PrepareMessages []*QBFTTrace        `ssz-max:"13"`
}

type QBFTTrace struct {
	Round        uint64      // same for
	BeaconRoot   phase0.Root `ssz-size:"32"`
	Signer       spectypes.OperatorID
	ReceivedTime time.Time
}

type PartialSigTrace struct {
	Type         spectypes.PartialSigMsgType
	BeaconRoot   phase0.Root `ssz-size:"32"`
	Signer       spectypes.OperatorID
	ReceivedTime time.Time
}

// Committee
type CommitteeDutyTrace struct {
	ConsensusTrace

	Slot phase0.Slot

	CommitteeID spectypes.CommitteeID  `ssz-size:"32"`
	OperatorIDs []spectypes.OperatorID `ssz-max:"13"`

	SyncCommittee []*SignerData `ssz-max:"1512"`
	Attester      []*SignerData `ssz-max:"1512"`
}

type SignerData struct {
	Signers      []spectypes.OperatorID `ssz-max:"13"`
	ReceivedTime time.Time
}
