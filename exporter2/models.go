package exporter2

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/ibft/storage"
)

// DecidedsQuery defines the transport-agnostic parameters for decided traces
// queries.
type DecidedsQuery struct {
	From    uint64
	To      uint64
	Roles   []spectypes.BeaconRole
	PubKeys []spectypes.ValidatorPK
	Indices []phase0.ValidatorIndex
}

func (q *DecidedsQuery) HasFilters() bool {
	return len(q.PubKeys) > 0 || len(q.Indices) > 0
}

// DecidedParticipant represents a single decided duty record in domain terms.
type DecidedParticipant struct {
	Role    spectypes.BeaconRole
	Slot    phase0.Slot
	Index   phase0.ValidatorIndex
	PubKey  spectypes.ValidatorPK
	Signers []spectypes.OperatorID
}

func DecidedParticipantFromRange(role spectypes.BeaconRole, entry storage.ParticipantsRangeEntry, index phase0.ValidatorIndex) DecidedParticipant {
	return DecidedParticipant{
		Role:    role,
		Slot:    entry.Slot,
		Index:   index,
		PubKey:  entry.PubKey,
		Signers: entry.Signers,
	}
}

// TraceDecidedsResult is the domain-level result for decided traces queries.
type TraceDecidedsResult struct {
	Participants []DecidedParticipant
	Errors       []error
}

// ValidatorTracesQuery defines the transport-agnostic parameters for validator
// traces queries.
type ValidatorTracesQuery struct {
	From    uint64
	To      uint64
	Roles   []spectypes.BeaconRole
	PubKeys []spectypes.ValidatorPK
	Indices []phase0.ValidatorIndex
}

func (q *ValidatorTracesQuery) HasFilters() bool {
	return len(q.PubKeys) > 0 || len(q.Indices) > 0
}

// ValidatorScheduleEntry represents a single per-validator schedule entry.
type ValidatorScheduleEntry struct {
	Slot      phase0.Slot
	Validator phase0.ValidatorIndex
	Roles     []spectypes.BeaconRole
}

// ValidatorCommitteeTrace is the domain representation of a validator duty trace
// enriched with an optional committee ID.
type ValidatorCommitteeTrace struct {
	exporter.ValidatorDutyTrace
	CommitteeID *spectypes.CommitteeID
}

// ValidatorTracesResult is the domain-level result for validator trace queries.
type ValidatorTracesResult struct {
	Traces   []ValidatorCommitteeTrace
	Schedule []ValidatorScheduleEntry
	Errors   []error
}

// CommitteeTracesQuery defines the transport-agnostic parameters for committee
// traces queries.
type CommitteeTracesQuery struct {
	From         uint64
	To           uint64
	CommitteeIDs []spectypes.CommitteeID
}

// CommitteeScheduleEntry represents a single per-committee schedule entry.
type CommitteeScheduleEntry struct {
	Slot        phase0.Slot
	CommitteeID spectypes.CommitteeID
	Roles       map[spectypes.BeaconRole][]phase0.ValidatorIndex
}

// CommitteeTracesResult is the domain-level result for committee trace queries.
type CommitteeTracesResult struct {
	Traces   []*exporter.CommitteeDutyTrace
	Schedule []CommitteeScheduleEntry
	Errors   []error
}
