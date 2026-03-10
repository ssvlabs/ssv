package exporter

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/api"
	audit "github.com/ssvlabs/ssv/operator/dutytracer/auditor"
)

type AuditorFindingsRequest struct {
	From uint64 `json:"from" minimum:"0"`
	To   uint64 `json:"to" minimum:"0"`

	Reason string `json:"reason,omitempty"`
	Role   string `json:"role,omitempty"`

	CommitteeID    string  `json:"committeeID,omitempty" format:"hex"`
	ValidatorIndex *uint64 `json:"validatorIndex,omitempty" format:"int64"`

	Limit int `json:"limit,omitempty"`
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

	q, err := toAuditQuery(&req)
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

func toAuditQuery(req *AuditorFindingsRequest) (audit.Query, error) {
	if req == nil {
		return audit.Query{}, fmt.Errorf("nil request")
	}
	q := audit.Query{
		From:  phase0.Slot(req.From),
		To:    phase0.Slot(req.To),
		Limit: req.Limit,
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

func isKnownReason(rc audit.ReasonCode) bool {
	switch rc {
	case audit.ReasonScheduleMissingIndex,
		audit.ReasonScheduleRoleBitMissing,
		audit.ReasonScheduleNotComputed,
		audit.ReasonScheduleComputeFailed,
		audit.ReasonScheduleJobDropped,
		audit.ReasonScheduleBeforeDutiesReady,
		audit.ReasonDutyFetchFailed,
		audit.ReasonDutyStoreIncomplete,
		audit.ReasonRPCFallbackFailed,
		audit.ReasonRPCFallbackSkipped,
		audit.ReasonRegistryIndexNotFound,
		audit.ReasonCommitteeLinkMissing,
		audit.ReasonCommitteeLinkMismatch,
		audit.ReasonRegistryCommitteeMismatch,
		audit.ReasonUnexpectedWireTrace,
		audit.ReasonRoleClassificationSuspect:
		return true
	default:
		return false
	}
}
