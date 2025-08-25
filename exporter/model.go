package exporter

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

//go:generate sszgen -include ../../vendor/github.com/attestantio/go-eth2-client/spec/phase0,../../vendor/github.com/ssvlabs/ssv-spec/types,../../vendor/github.com/ssvlabs/ssv-spec/qbft --path model.go --objs ValidatorDutyTrace,CommitteeDutyTrace,DiskMsg
type ValidatorDutyTrace struct {
	ConsensusTrace

	Slot      phase0.Slot
	Role      spectypes.BeaconRole
	Validator phase0.ValidatorIndex

	ProposalData []byte `ssz-max:"4194532"`

	Pre  []*PartialSigTrace `ssz-max:"13"`
	Post []*PartialSigTrace `ssz-max:"13"`
}

type ConsensusTrace struct {
	Rounds   []*RoundTrace   `ssz-max:"15"`
	Decideds []*DecidedTrace `ssz-max:"256"`
}

type DecidedTrace struct {
	Round        uint64
	BeaconRoot   phase0.Root            `ssz-size:"32"`
	Signers      []spectypes.OperatorID `ssz-max:"13"`
	ReceivedTime uint64
}

type RoundTrace struct {
	Proposer spectypes.OperatorID

	ProposalTrace *ProposalTrace

	Prepares     []*QBFTTrace        `ssz-max:"13"`
	Commits      []*QBFTTrace        `ssz-max:"13"`
	RoundChanges []*RoundChangeTrace `ssz-max:"13"`
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
	ReceivedTime uint64
}

type PartialSigTrace struct {
	Type         spectypes.PartialSigMsgType
	BeaconRoot   phase0.Root `ssz-size:"32"`
	Signer       spectypes.OperatorID
	ReceivedTime uint64
}

// Committee
type CommitteeDutyTrace struct {
	ConsensusTrace

	Slot phase0.Slot

	CommitteeID spectypes.CommitteeID  `ssz-size:"32"`
	OperatorIDs []spectypes.OperatorID `ssz-max:"13"`

	ProposalData []byte `ssz-max:"4194532"`

	SyncCommittee []*SignerData `ssz-max:"1512"`
	Attester      []*SignerData `ssz-max:"1512"`
}

type SignerData struct {
	Signer       spectypes.OperatorID
	ValidatorIdx []phase0.ValidatorIndex `ssz-max:"1000"`
	ReceivedTime uint64
}

type CommitteeDutyLink struct {
	ValidatorIndex phase0.ValidatorIndex
	CommitteeID    spectypes.CommitteeID
}

// used in benchmarks
type DiskMsg struct {
	Signed spectypes.SignedSSVMessage
	Spec   spectypes.SSVMessage
	Kind   uint8 // 0 - qbft, 1 - sig
	Qbft   specqbft.Message
	Sig    spectypes.PartialSignatureMessages
}
