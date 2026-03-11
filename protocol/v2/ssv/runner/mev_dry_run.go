package runner

import (
	"context"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// MEVDryRunService runs the in-node MEV flow in parallel to the legacy proposer flow,
// and stores per-slot comparison results for later inspection.
//
// Implementations must be low-overhead and thread-safe.
type MEVDryRunService interface {
	// StartShadowGetHeader begins a shadow "get_header" operation for (slot, pubkey).
	// The returned channel yields exactly one result and is then closed.
	StartShadowGetHeader(ctx context.Context, slot phase0.Slot, pubkey phase0.BLSPubKey) <-chan MEVShadowGetHeaderResult
	// RecordComparison stores a completed comparison for later querying.
	RecordComparison(ctx context.Context, c MEVDryRunComparison)
}

// MEVDryRunExactParentService is an optional extension interface for implementations that can run
// a shadow get_header using an explicit parent_hash (e.g. derived from the baseline block).
type MEVDryRunExactParentService interface {
	StartShadowGetHeaderWithParentHash(ctx context.Context, slot phase0.Slot, parentHash phase0.Hash32, pubkey phase0.BLSPubKey) <-chan MEVShadowGetHeaderResult
}

const (
	MEVShadowResultBid     = "bid"
	MEVShadowResultNoBid   = "no_bid"
	MEVShadowResultError   = "error"
	MEVShadowResultHeadErr = "head_error"
	MEVShadowResultTimeout = "timeout"
	MEVBaselineResultOK    = "ok"
	MEVBaselineResultError = "error"
)

type MEVShadowGetHeaderResult struct {
	StartedAt time.Time     `json:"started_at"`
	Took      time.Duration `json:"took"`

	HeadHashTook time.Duration `json:"head_hash_took,omitempty"`

	ParentHashHex string `json:"parent_hash,omitempty"`

	// Result is one of: "bid", "no_bid", "error", "head_error", "timeout".
	Result string `json:"result"`
	// Cache is "hit" or "miss" when Result is bid/no_bid/error. Empty when head_error/timeout.
	Cache string `json:"cache,omitempty"`

	RelayHost string  `json:"relay_host,omitempty"`
	ValueETH  float64 `json:"value_eth,omitempty"`
}

type MEVBaselineGetBlockResult struct {
	StartedAt time.Time     `json:"started_at"`
	Took      time.Duration `json:"took"`

	// Result is one of: "ok", "error".
	Result  string `json:"result"`
	Blinded bool   `json:"blinded,omitempty"`
}

type MEVDryRunComparison struct {
	Slot            phase0.Slot           `json:"slot"`
	ValidatorIndex  phase0.ValidatorIndex `json:"validator_index"`
	ValidatorPubkey string                `json:"validator_pubkey,omitempty"`

	SlotOffsetMs int64 `json:"slot_offset_ms,omitempty"`

	BaselineStartOffsetMs  int64 `json:"baseline_start_offset_ms,omitempty"`
	BaselineFinishOffsetMs int64 `json:"baseline_finish_offset_ms,omitempty"`

	ShadowStartOffsetMs  int64 `json:"shadow_start_offset_ms,omitempty"`
	ShadowFinishOffsetMs int64 `json:"shadow_finish_offset_ms,omitempty"`

	ShadowExactStartOffsetMs  int64 `json:"shadow_exact_start_offset_ms,omitempty"`
	ShadowExactFinishOffsetMs int64 `json:"shadow_exact_finish_offset_ms,omitempty"`

	BaselineExecParentHash string `json:"baseline_exec_parent_hash,omitempty"`
	ParentHashMatch        bool   `json:"parent_hash_match,omitempty"`

	Baseline MEVBaselineGetBlockResult `json:"baseline"`
	Shadow   MEVShadowGetHeaderResult  `json:"shadow"`

	ShadowExactParent *MEVShadowGetHeaderResult `json:"shadow_exact_parent,omitempty"`
	RecoveredBid      bool                      `json:"recovered_bid,omitempty"`
}
