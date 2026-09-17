package traces

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

const (
	// MaxPartialSigEntries bounds ValidatorDutyTrace.Pre and .Post, which hold one entry per partial
	// signature per signer. The checks below tie it to the worst cases message validation admits on
	// the largest committee, so a limit that outgrows the bound fails the build rather than leaving a
	// trace that cannot be encoded at eviction.
	MaxPartialSigEntries = 256
	// maxSigners is the largest committee.
	maxSigners = 13
)

var (
	// Proposer preferences: distinct preference roots plus one request-auth root per builder, per signer.
	_ [MaxPartialSigEntries - maxSigners*(gloas.MaxProposerPreferencesDistinctRoots+gloas.MaxRequestAuthDistinctRoots)]struct{}
	// Sync-committee contribution: up to maxSigners entries per packet, per signer.
	_ [MaxPartialSigEntries - maxSigners*maxSigners]struct{}
)

// NOTE: model_encoding.go is HAND-MAINTAINED — `go generate` cannot regenerate it.
// Every available sszgen (ferranbt/fastssz v1.0.0 in tool.mod, public v0.1.3, and the
// prysmaticlabs fork) rejects CommitteeDutyTrace.Role (spectypes.RunnerRole is int32, and
// SSZ has no signed-int type) and mishandles SignerData.ValidatorIdx
// ([]phase0.ValidatorIndex, a named-uint64 slice the current tool emits as []uint64).
// The committed encoding came from an older/custom generator (int32->uint64 cast,
// make([]NamedType, n)); edit model_encoding.go by hand when these structs change.
// Objects, once a compatible sszgen exists again: ValidatorDutyTrace, CommitteeDutyTrace, DiskMsg.
type ValidatorDutyTrace struct {
	ConsensusTrace

	Slot      phase0.Slot
	Role      spectypes.BeaconRole
	Validator phase0.ValidatorIndex

	ProposalData []byte `ssz-max:"4194532"`

	// Pre and Post hold one entry per partial signature per signer (MaxPartialSigEntries), so a packet
	// with several entries (the Gloas proposer's block and envelope roots, a sync-committee contribution's
	// subnets) and a signer that re-emits under new roots (proposer preferences, request auth) all fit.
	// Traces with more than one entry per operator are unreadable by binaries before store format 2
	// (see store.FormatVersion).
	Pre  []*PartialSigTrace `ssz-max:"256"`
	Post []*PartialSigTrace `ssz-max:"256"`
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

// DeepCopy returns a deep copy of the DecidedTrace.
func (trace *DecidedTrace) DeepCopy() *DecidedTrace {
	if trace == nil {
		return nil
	}
	signers := make([]spectypes.OperatorID, len(trace.Signers))
	copy(signers, trace.Signers)
	return &DecidedTrace{
		Round:        trace.Round,
		BeaconRoot:   trace.BeaconRoot,
		Signers:      signers,
		ReceivedTime: trace.ReceivedTime,
	}
}

type RoundTrace struct {
	Proposer spectypes.OperatorID

	ProposalTrace *ProposalTrace

	Prepares     []*QBFTTrace        `ssz-max:"13"`
	Commits      []*QBFTTrace        `ssz-max:"13"`
	RoundChanges []*RoundChangeTrace `ssz-max:"13"`
}

// DeepCopy returns a deep copy of the RoundTrace.
func (round *RoundTrace) DeepCopy() *RoundTrace {
	if round == nil {
		return nil
	}
	return &RoundTrace{
		Proposer:      round.Proposer,
		Prepares:      deepCopySlice(round.Prepares),
		ProposalTrace: round.ProposalTrace.DeepCopy(),
		Commits:       deepCopySlice(round.Commits),
		RoundChanges:  deepCopySlice(round.RoundChanges),
	}
}

type RoundChangeTrace struct {
	QBFTTrace
	PreparedRound   uint64
	PrepareMessages []*QBFTTrace `ssz-max:"13"`
}

// DeepCopy returns a deep copy of the RoundChangeTrace.
func (trace *RoundChangeTrace) DeepCopy() *RoundChangeTrace {
	if trace == nil {
		return nil
	}
	return &RoundChangeTrace{
		QBFTTrace: QBFTTrace{
			Round:        trace.Round,
			BeaconRoot:   trace.BeaconRoot,
			Signer:       trace.Signer,
			ReceivedTime: trace.ReceivedTime,
		},
		PreparedRound:   trace.PreparedRound,
		PrepareMessages: deepCopySlice(trace.PrepareMessages),
	}
}

type ProposalTrace struct {
	QBFTTrace
	RoundChanges    []*RoundChangeTrace `ssz-max:"13"`
	PrepareMessages []*QBFTTrace        `ssz-max:"13"`
}

// DeepCopy returns a deep copy of the ProposalTrace.
func (trace *ProposalTrace) DeepCopy() *ProposalTrace {
	if trace == nil {
		return nil
	}
	return &ProposalTrace{
		QBFTTrace: QBFTTrace{
			Round:        trace.Round,
			BeaconRoot:   trace.BeaconRoot,
			Signer:       trace.Signer,
			ReceivedTime: trace.ReceivedTime,
		},
		RoundChanges:    deepCopySlice(trace.RoundChanges),
		PrepareMessages: deepCopySlice(trace.PrepareMessages),
	}
}

type QBFTTrace struct {
	Round        uint64
	BeaconRoot   phase0.Root `ssz-size:"32"`
	Signer       spectypes.OperatorID
	ReceivedTime uint64
}

// DeepCopy returns a deep copy of the QBFTTrace.
func (trace *QBFTTrace) DeepCopy() *QBFTTrace {
	if trace == nil {
		return nil
	}
	return &QBFTTrace{
		Round:        trace.Round,
		Signer:       trace.Signer,
		ReceivedTime: trace.ReceivedTime,
		BeaconRoot:   trace.BeaconRoot,
	}
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
	Role spectypes.RunnerRole

	CommitteeID spectypes.CommitteeID  `ssz-size:"32"`
	OperatorIDs []spectypes.OperatorID `ssz-max:"13"`

	ProposalData []byte `ssz-max:"4194532"`

	SyncCommittee []*SignerData `ssz-max:"1512"`
	Attester      []*SignerData `ssz-max:"1512"`
}

// DeepCopy returns a deep copy of the CommitteeDutyTrace.
// IMPORTANT: Caller must hold appropriate locks when copying shared/mutable data structures
// to prevent concurrent modifications during the copy operation.
func (trace *CommitteeDutyTrace) DeepCopy() *CommitteeDutyTrace {
	if trace == nil {
		return nil
	}

	opIDs := make([]spectypes.OperatorID, len(trace.OperatorIDs))
	copy(opIDs, trace.OperatorIDs)

	pd := make([]byte, len(trace.ProposalData))
	copy(pd, trace.ProposalData)

	return &CommitteeDutyTrace{
		ConsensusTrace: ConsensusTrace{
			Rounds:   deepCopySlice(trace.Rounds),
			Decideds: deepCopySlice(trace.Decideds),
		},
		Slot:          trace.Slot,
		Role:          trace.Role,
		CommitteeID:   trace.CommitteeID,
		OperatorIDs:   opIDs,
		SyncCommittee: deepCopySlice(trace.SyncCommittee),
		Attester:      deepCopySlice(trace.Attester),
		ProposalData:  pd,
	}
}

type SignerData struct {
	Signer       spectypes.OperatorID
	ValidatorIdx []phase0.ValidatorIndex `ssz-max:"3000"`
	ReceivedTime uint64
}

// DeepCopy returns a deep copy of the SignerData.
func (data *SignerData) DeepCopy() *SignerData {
	if data == nil {
		return nil
	}

	validatorIdx := make([]phase0.ValidatorIndex, len(data.ValidatorIdx))
	copy(validatorIdx, data.ValidatorIdx)

	return &SignerData{
		Signer:       data.Signer,
		ValidatorIdx: validatorIdx,
		ReceivedTime: data.ReceivedTime,
	}
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

// DeepCopy returns a deep copy of the ValidatorDutyTrace.
// IMPORTANT: Caller must hold appropriate locks when copying shared/mutable data structures
// to prevent concurrent modifications during the copy operation.
func (trace *ValidatorDutyTrace) DeepCopy() *ValidatorDutyTrace {
	if trace == nil {
		return nil
	}

	pd := make([]byte, len(trace.ProposalData))
	copy(pd, trace.ProposalData)

	return &ValidatorDutyTrace{
		ConsensusTrace: ConsensusTrace{
			Rounds:   deepCopySlice(trace.Rounds),
			Decideds: deepCopySlice(trace.Decideds),
		},
		Slot:         trace.Slot,
		Role:         trace.Role,
		Validator:    trace.Validator,
		Pre:          deepCopySlice(trace.Pre),
		Post:         deepCopySlice(trace.Post),
		ProposalData: pd,
	}
}

// DeepCopy returns a deep copy of the PartialSigTrace.
func (trace *PartialSigTrace) DeepCopy() *PartialSigTrace {
	if trace == nil {
		return nil
	}
	return &PartialSigTrace{
		Type:         trace.Type,
		BeaconRoot:   trace.BeaconRoot,
		Signer:       trace.Signer,
		ReceivedTime: trace.ReceivedTime,
	}
}

type hasDeepCopy[T any] interface {
	DeepCopy() T
}

func deepCopySlice[T hasDeepCopy[T]](value []T) []T {
	cp := make([]T, len(value))
	for i, v := range value {
		cp[i] = v.DeepCopy()
	}
	return cp
}
