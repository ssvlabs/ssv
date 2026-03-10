package auditor

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// Role is the auditor-facing role dimension for committee traces.
// It intentionally mirrors the /traces/committee post-consensus buckets.
type Role string

const (
	RoleAttester      Role = "attester"
	RoleSyncCommittee Role = "sync_committee"
)

type ReasonCode string

const (
	ReasonScheduleMissingIndex      ReasonCode = "SCHEDULE_MISSING_INDEX"
	ReasonScheduleRoleBitMissing    ReasonCode = "SCHEDULE_ROLE_BIT_MISSING"
	ReasonScheduleNotComputed       ReasonCode = "SCHEDULE_NOT_COMPUTED"
	ReasonScheduleComputeFailed     ReasonCode = "SCHEDULE_COMPUTE_FAILED"
	ReasonScheduleJobDropped        ReasonCode = "SCHEDULE_JOB_DROPPED"
	ReasonScheduleBeforeDutiesReady ReasonCode = "SCHEDULE_BEFORE_DUTIES_READY"
	ReasonScheduleReadFailed        ReasonCode = "SCHEDULE_READ_FAILED"

	ReasonDutyFetchFailed        ReasonCode = "DUTY_FETCH_FAILED"
	ReasonDutyStoreIncomplete    ReasonCode = "DUTY_STORE_INCOMPLETE"
	ReasonRPCFallbackFailed      ReasonCode = "RPC_FALLBACK_FAILED"
	ReasonRPCFallbackSkipped     ReasonCode = "RPC_FALLBACK_SKIPPED"
	ReasonLinksReadFailed        ReasonCode = "LINKS_READ_FAILED"
	ReasonTraceSlotMisattributed ReasonCode = "TRACE_SLOT_MISATTRIBUTED"

	ReasonRegistryIndexNotFound     ReasonCode = "REGISTRY_INDEX_NOT_FOUND"
	ReasonCommitteeLinkMissing      ReasonCode = "COMMITTEE_LINK_MISSING"
	ReasonCommitteeLinkMismatch     ReasonCode = "COMMITTEE_LINK_MISMATCH"
	ReasonRegistryCommitteeMismatch ReasonCode = "REGISTRY_COMMITTEE_MISMATCH"

	ReasonUnexpectedWireTrace       ReasonCode = "UNEXPECTED_WIRE_TRACE"
	ReasonRoleClassificationSuspect ReasonCode = "ROLE_CLASSIFICATION_SUSPECT"
)

var ErrAuditorDisabled = errors.New("auditor disabled")

type Status struct {
	Enabled bool `json:"enabled"`

	DelaySlots      uint64 `json:"delaySlots"`
	LastAuditedSlot uint64 `json:"lastAuditedSlot"`
	RetentionSlots  uint64 `json:"retentionSlots"`

	RPCFallbackEnabled bool `json:"rpcFallbackEnabled"`

	MinStoredSlot *uint64 `json:"minStoredSlot,omitempty"`
	MaxStoredSlot *uint64 `json:"maxStoredSlot,omitempty"`
}

// Finding is a single auditor-detected mismatch with evidence attached.
// It is persisted (2 weeks retention) and exposed via HTTP for later analysis.
type Finding struct {
	Version   uint32    `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	// ID is a deterministic fingerprint used for log correlation even when
	// persistence is capped or unavailable.
	ID string `json:"id,omitempty"`
	// Key is a stable identifier assigned when persisted, intended for
	// correlation between logs and the findings store.
	Key    string     `json:"key,omitempty"`
	Slot   uint64     `json:"slot"`
	Epoch  uint64     `json:"epoch"`
	Period *uint64    `json:"period,omitempty"`
	Reason ReasonCode `json:"reason"`

	Role           *Role   `json:"role,omitempty"`
	ValidatorIndex *uint64 `json:"validatorIndex,omitempty"`
	CommitteeID    *string `json:"committeeID,omitempty"` // hex(32 bytes)

	// Evidence holds structured debugging details.
	Evidence Evidence `json:"evidence"`
}

type Evidence struct {
	Observed ObservedEvidence `json:"observed"`

	PersistedSchedule ScheduleEvidence `json:"persistedSchedule"`
	Expected          ExpectedEvidence `json:"expected"`

	Registry RegistryEvidence `json:"registry"`
	Links    LinksEvidence    `json:"links"`

	Pipeline PipelineEvidence `json:"pipeline"`
}

type ObservedEvidence struct {
	SignersCount int      `json:"signersCount,omitempty"`
	Signers      []uint64 `json:"signers,omitempty"` // operator IDs (truncated/sampled at source)

	MessagesCount int `json:"messagesCount,omitempty"`

	ReceivedMinMs *uint64 `json:"receivedMinMs,omitempty"`
	ReceivedMaxMs *uint64 `json:"receivedMaxMs,omitempty"`
}

type ScheduleEvidence struct {
	ScheduleSize int     `json:"scheduleSize,omitempty"`
	HasIndex     bool    `json:"hasIndex"`
	MaskBits     []uint8 `json:"maskBits,omitempty"` // compact list of set bits in role mask
	HasRole      bool    `json:"hasRole"`

	ReadOK    bool   `json:"readOk"`
	ReadError string `json:"readError,omitempty"`
}

type ExpectedEvidence struct {
	ByDutyStore *bool `json:"byDutyStore,omitempty"`

	RPCFallback RPCFallbackEvidence `json:"rpcFallback"`

	ExpectedOtherRole bool `json:"expectedOtherRole,omitempty"`
}

type RPCFallbackEvidence struct {
	Enabled bool   `json:"enabled"`
	Used    bool   `json:"used"`
	OK      *bool  `json:"ok,omitempty"`
	Error   string `json:"error,omitempty"`

	AttesterExpectedSlot *uint64 `json:"attesterExpectedSlot,omitempty"`
}

type RegistryEvidence struct {
	ValidatorKnown        bool    `json:"validatorKnown"`
	HasBeaconMetadata     *bool   `json:"hasBeaconMetadata,omitempty"`
	MinParticipationEpoch *uint64 `json:"minParticipationEpoch,omitempty"`

	ExpectedCommitteeID *string `json:"expectedCommitteeID,omitempty"`
}

type LinksEvidence struct {
	LinkPresent       bool    `json:"linkPresent"`
	LinkedCommitteeID *string `json:"linkedCommitteeID,omitempty"`

	ReadOK    bool   `json:"readOk"`
	ReadError string `json:"readError,omitempty"`
}

type PipelineEvidence struct {
	ScheduleJobDroppedCount uint64 `json:"scheduleJobDroppedCount,omitempty"`

	ScheduleCompute *ScheduleComputeEvidence `json:"scheduleCompute,omitempty"`
	DutyFetch       *DutyFetchEvidence       `json:"dutyFetch,omitempty"`
}

type ScheduleComputeEvidence struct {
	ComputedAt           time.Time `json:"computedAt"`
	OK                   bool      `json:"ok"`
	Error                string    `json:"error,omitempty"`
	ComputedScheduleSize int       `json:"computedScheduleSize,omitempty"`
}

type DutyFetchEvidence struct {
	At        time.Time `json:"at"`
	OK        bool      `json:"ok"`
	Error     string    `json:"error,omitempty"`
	TookMs    int64     `json:"tookMs,omitempty"`
	Requested int       `json:"requested,omitempty"`
	Returned  int       `json:"returned,omitempty"`
}

func (f *Finding) MarshalJSON() ([]byte, error) {
	type alias Finding
	return json.Marshal((*alias)(f))
}

func committeeIDHex(id spectypes.CommitteeID) string {
	return hex.EncodeToString(id[:])
}

func maskToBits(mask uint8) []uint8 {
	if mask == 0 {
		return nil
	}
	out := make([]uint8, 0, 8)
	for i := uint8(0); i < 8; i++ {
		if mask&(uint8(1)<<i) != 0 {
			out = append(out, i)
		}
	}
	return out
}

func ptrBool(v bool) *bool { return &v }
