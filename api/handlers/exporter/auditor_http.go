package exporter

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/api"
	audit "github.com/ssvlabs/ssv/operator/dutytracer/auditor"
)

type AuditorFindingsRequest struct {
	From  *uint64 `json:"from,omitempty" minimum:"0"`
	To    *uint64 `json:"to,omitempty" minimum:"0"`
	LastN *uint64 `json:"lastN,omitempty" minimum:"0"`

	Reason string `json:"reason,omitempty"`
	Role   string `json:"role,omitempty"`

	CommitteeID    string  `json:"committeeID,omitempty" format:"hex"`
	ValidatorIndex *uint64 `json:"validatorIndex,omitempty" format:"int64"`

	Limit  int    `json:"limit,omitempty"`
	Order  string `json:"order,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

type AuditorFindingsResponse struct {
	Data   []*audit.Finding `json:"data"`
	Errors []string         `json:"errors,omitempty"`
}

// AuditorFindings godoc
// @Summary Retrieve auditor findings
// @Description Returns persisted trace<->schedule mismatch findings (if auditor is enabled).
// @Tags Exporter
// @Accept json
// @Produce json
// @Param request query AuditorFindingsRequest false "Filters as query parameters"
// @Param request body AuditorFindingsRequest false "Filters as JSON body"
// @Success 200 {object} AuditorFindingsResponse
// @Failure 400 {object} api.ErrorResponse
// @Failure 501 {object} api.ErrorResponse "Auditor disabled"
// @Failure 500 {object} api.ErrorResponse
// @Router /v1/exporter/auditor/findings [get]
// @Router /v1/exporter/auditor/findings [post]
func (e *Exporter) AuditorFindings(w http.ResponseWriter, r *http.Request) error {
	if e.audit == nil {
		return toApiError(e.logger, r, "auditor_findings", http.StatusNotImplemented, nil, fmt.Errorf("auditor disabled"))
	}

	var req AuditorFindingsRequest
	if err := api.Bind(r, &req); err != nil {
		return toApiError(e.logger, r, "auditor_findings", http.StatusBadRequest, req, err)
	}

	st, err := e.audit.AuditStatus()
	if err != nil {
		if errors.Is(err, audit.ErrAuditorDisabled) {
			return toApiError(e.logger, r, "auditor_findings", http.StatusNotImplemented, req, err)
		}
		return toApiError(e.logger, r, "auditor_findings", http.StatusInternalServerError, req, err)
	}

	q, err := toAuditQuery(&req, st.LastAuditedSlot)
	if err != nil {
		return toApiError(e.logger, r, "auditor_findings", http.StatusBadRequest, req, err)
	}

	res, err := e.audit.QueryAuditFindings(q)
	if err != nil {
		if errors.Is(err, audit.ErrAuditorDisabled) {
			return toApiError(e.logger, r, "auditor_findings", http.StatusNotImplemented, req, err)
		}
		return toApiError(e.logger, r, "auditor_findings", http.StatusInternalServerError, req, err)
	}

	return api.Render(w, r, &AuditorFindingsResponse{Data: res.Findings})
}

func toAuditQuery(req *AuditorFindingsRequest, lastAuditedSlot uint64) (audit.Query, error) {
	if req == nil {
		return audit.Query{}, fmt.Errorf("nil request")
	}

	from, to, err := resolveSlotRange(req.From, req.To, req.LastN, lastAuditedSlot)
	if err != nil {
		return audit.Query{}, err
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	q := audit.Query{
		From:  phase0.Slot(from),
		To:    phase0.Slot(to),
		Limit: limit,
	}
	if req.Order != "" {
		q.Order = strings.TrimSpace(req.Order)
	} else {
		q.Order = "desc"
	}
	if req.Cursor != "" {
		c := strings.TrimSpace(req.Cursor)
		q.Cursor = &c
	}

	if req.Reason != "" {
		rc := audit.ReasonCode(strings.TrimSpace(req.Reason))
		if !isKnownReason(rc) {
			return audit.Query{}, fmt.Errorf("unknown reason: %s", req.Reason)
		}
		q.Reason = &rc
	}
	if req.Role != "" {
		role := audit.Role(strings.TrimSpace(req.Role))
		switch role {
		case audit.RoleAttester, audit.RoleSyncCommittee:
			q.Role = &role
		default:
			return audit.Query{}, fmt.Errorf("unknown role: %s", req.Role)
		}
	}
	if req.CommitteeID != "" {
		h := strings.TrimSpace(req.CommitteeID)
		if _, err := hex.DecodeString(h); err != nil {
			return audit.Query{}, fmt.Errorf("invalid committeeID hex: %w", err)
		}
		if len(h) != 64 {
			return audit.Query{}, fmt.Errorf("invalid committeeID length: %d", len(h))
		}
		q.CommitteeIDHex = &h
	}
	if req.ValidatorIndex != nil {
		q.ValidatorIndex = req.ValidatorIndex
	}

	return q, nil
}

func resolveSlotRange(from, to, lastN *uint64, lastAuditedSlot uint64) (uint64, uint64, error) {
	const defaultLastN = uint64(256)
	const maxRange = uint64(4096)

	if lastN != nil && *lastN == 0 {
		lastN = nil
	}
	if lastN == nil && from != nil && to != nil && *from == 0 && *to == 0 {
		from, to = nil, nil
	}

	// Default: last N slots ending at lastAuditedSlot.
	if from == nil && to == nil && lastN == nil {
		n := defaultLastN
		if lastAuditedSlot < n {
			return 0, lastAuditedSlot, nil
		}
		return lastAuditedSlot - n, lastAuditedSlot, nil
	}

	// lastN provided: infer missing edges from lastAuditedSlot.
	if lastN != nil {
		n := *lastN
		if n == 0 {
			return 0, 0, fmt.Errorf("lastN must be > 0")
		}
		end := lastAuditedSlot
		if to != nil {
			end = *to
		}
		var start uint64
		if end > n {
			start = end - n
		}
		if from != nil {
			start = *from
		}
		if end < start {
			return 0, 0, fmt.Errorf("'to' must be >= 'from'")
		}
		if end-start > maxRange {
			return 0, 0, fmt.Errorf("slot range too large: %d (max %d)", end-start, maxRange)
		}
		return start, end, nil
	}

	// Explicit range: require both endpoints.
	if from == nil || to == nil {
		return 0, 0, fmt.Errorf("both 'from' and 'to' must be provided (or use 'lastN')")
	}
	if *to < *from {
		return 0, 0, fmt.Errorf("'to' must be >= 'from'")
	}
	if *to-*from > maxRange {
		return 0, 0, fmt.Errorf("slot range too large: %d (max %d)", *to-*from, maxRange)
	}
	return *from, *to, nil
}

type AuditorStatusResponse struct {
	Data audit.Status `json:"data"`
}

// AuditorStatus godoc
// @Summary Retrieve auditor status
// @Description Returns basic auditor status (if auditor is enabled).
// @Tags Exporter
// @Accept json
// @Produce json
// @Success 200 {object} AuditorStatusResponse
// @Failure 501 {object} api.ErrorResponse "Auditor disabled"
// @Failure 500 {object} api.ErrorResponse
// @Router /v1/exporter/auditor/status [get]
func (e *Exporter) AuditorStatus(w http.ResponseWriter, r *http.Request) error {
	if e.audit == nil {
		return toApiError(e.logger, r, "auditor_status", http.StatusNotImplemented, nil, fmt.Errorf("auditor disabled"))
	}
	st, err := e.audit.AuditStatus()
	if err != nil {
		if errors.Is(err, audit.ErrAuditorDisabled) {
			return toApiError(e.logger, r, "auditor_status", http.StatusNotImplemented, nil, err)
		}
		return toApiError(e.logger, r, "auditor_status", http.StatusInternalServerError, nil, err)
	}
	return api.Render(w, r, &AuditorStatusResponse{Data: st})
}

type AuditorSummaryRequest struct {
	From  *uint64 `json:"from,omitempty" minimum:"0"`
	To    *uint64 `json:"to,omitempty" minimum:"0"`
	LastN *uint64 `json:"lastN,omitempty" minimum:"0"`

	// MaxFindings limits how many stored findings are scanned to build the summary.
	// This is a safety valve; defaults to 200000 and is capped to 500000.
	MaxFindings int `json:"maxFindings,omitempty"`
}

type AuditorSummaryEntry struct {
	Reason     string            `json:"reason"`
	Total      uint64            `json:"total"`
	RoleCounts map[string]uint64 `json:"roleCounts,omitempty"`

	RPCUsed   uint64 `json:"rpcUsed"`
	RPCOK     uint64 `json:"rpcOk"`
	RPCErrors uint64 `json:"rpcErrors"`
}

type AuditorSummaryResponse struct {
	From uint64                `json:"from"`
	To   uint64                `json:"to"`
	Data []AuditorSummaryEntry `json:"data"`
}

// AuditorSummary godoc
// @Summary Retrieve auditor summary
// @Description Returns a summary of stored findings grouped by reason (and role counts) over a slot range.
// @Tags Exporter
// @Accept json
// @Produce json
// @Param request query AuditorSummaryRequest false "Filters as query parameters"
// @Param request body AuditorSummaryRequest false "Filters as JSON body"
// @Success 200 {object} AuditorSummaryResponse
// @Failure 400 {object} api.ErrorResponse
// @Failure 501 {object} api.ErrorResponse "Auditor disabled"
// @Failure 500 {object} api.ErrorResponse
// @Router /v1/exporter/auditor/summary [get]
// @Router /v1/exporter/auditor/summary [post]
func (e *Exporter) AuditorSummary(w http.ResponseWriter, r *http.Request) error {
	if e.audit == nil {
		return toApiError(e.logger, r, "auditor_summary", http.StatusNotImplemented, nil, fmt.Errorf("auditor disabled"))
	}

	var req AuditorSummaryRequest
	if err := api.Bind(r, &req); err != nil {
		return toApiError(e.logger, r, "auditor_summary", http.StatusBadRequest, req, err)
	}

	st, err := e.audit.AuditStatus()
	if err != nil {
		if errors.Is(err, audit.ErrAuditorDisabled) {
			return toApiError(e.logger, r, "auditor_summary", http.StatusNotImplemented, req, err)
		}
		return toApiError(e.logger, r, "auditor_summary", http.StatusInternalServerError, req, err)
	}

	from, to, err := resolveSlotRange(req.From, req.To, req.LastN, st.LastAuditedSlot)
	if err != nil {
		return toApiError(e.logger, r, "auditor_summary", http.StatusBadRequest, req, err)
	}

	maxFindings := req.MaxFindings
	if maxFindings <= 0 {
		maxFindings = 200000
	}
	if maxFindings > 500000 {
		maxFindings = 500000
	}

	res, err := e.audit.QueryAuditFindings(audit.Query{
		From:  phase0.Slot(from),
		To:    phase0.Slot(to),
		Limit: maxFindings,
	})
	if err != nil {
		if errors.Is(err, audit.ErrAuditorDisabled) {
			return toApiError(e.logger, r, "auditor_summary", http.StatusNotImplemented, req, err)
		}
		return toApiError(e.logger, r, "auditor_summary", http.StatusInternalServerError, req, err)
	}

	type acc struct {
		entry AuditorSummaryEntry
	}
	m := make(map[string]*acc)
	for _, f := range res.Findings {
		if f == nil {
			continue
		}
		rkey := string(f.Reason)
		a2, ok := m[rkey]
		if !ok {
			a2 = &acc{entry: AuditorSummaryEntry{Reason: rkey, RoleCounts: make(map[string]uint64)}}
			m[rkey] = a2
		}
		a2.entry.Total++
		if f.Role != nil {
			a2.entry.RoleCounts[string(*f.Role)]++
		}
		if f.Evidence.Expected.RPCFallback.Used {
			a2.entry.RPCUsed++
			if f.Evidence.Expected.RPCFallback.OK != nil {
				if *f.Evidence.Expected.RPCFallback.OK {
					a2.entry.RPCOK++
				} else {
					a2.entry.RPCErrors++
				}
			}
		}
	}

	out := make([]AuditorSummaryEntry, 0, len(m))
	for _, a2 := range m {
		if len(a2.entry.RoleCounts) == 0 {
			a2.entry.RoleCounts = nil
		}
		out = append(out, a2.entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Total > out[j].Total })

	return api.Render(w, r, &AuditorSummaryResponse{From: from, To: to, Data: out})
}

func isKnownReason(rc audit.ReasonCode) bool {
	switch rc {
	case audit.ReasonScheduleMissingIndex,
		audit.ReasonScheduleRoleBitMissing,
		audit.ReasonScheduleNotComputed,
		audit.ReasonScheduleComputeFailed,
		audit.ReasonScheduleJobDropped,
		audit.ReasonScheduleBeforeDutiesReady,
		audit.ReasonScheduleReadFailed,
		audit.ReasonDutyFetchFailed,
		audit.ReasonDutyStoreIncomplete,
		audit.ReasonRPCFallbackFailed,
		audit.ReasonRPCFallbackSkipped,
		audit.ReasonLinksReadFailed,
		audit.ReasonRegistryIndexNotFound,
		audit.ReasonCommitteeLinkMissing,
		audit.ReasonCommitteeLinkMismatch,
		audit.ReasonRegistryCommitteeMismatch,
		audit.ReasonUnexpectedWireTrace,
		audit.ReasonRoleClassificationSuspect,
		audit.ReasonTraceSlotMisattributed:
		return true
	default:
		return false
	}
}
