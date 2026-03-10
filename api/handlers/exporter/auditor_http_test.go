package exporter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	audit "github.com/ssvlabs/ssv/operator/dutytracer/auditor"
)

type fakeAuditAccessor struct {
	status audit.Status

	lastQuery audit.Query
	findings  []*audit.Finding
}

func (f *fakeAuditAccessor) AuditStatus() (audit.Status, error) { return f.status, nil }

func (f *fakeAuditAccessor) QueryAuditFindings(q audit.Query) (audit.QueryResult, error) {
	f.lastQuery = q
	return audit.QueryResult{Findings: f.findings}, nil
}

func TestAuditorFindings_DefaultRangeUsesLastAuditedSlot(t *testing.T) {
	a := &fakeAuditAccessor{status: audit.Status{Enabled: true, LastAuditedSlot: 1000}}
	e := &Exporter{logger: zap.NewNop(), audit: a}

	req := httptest.NewRequest(http.MethodGet, "/v1/exporter/auditor/findings", nil)
	rec := httptest.NewRecorder()
	require.NoError(t, e.AuditorFindings(rec, req))

	require.Equal(t, phase0.Slot(744), a.lastQuery.From)
	require.Equal(t, phase0.Slot(1000), a.lastQuery.To)
	require.Equal(t, 100, a.lastQuery.Limit)
}

func TestAuditorStatus_ReturnsStatus(t *testing.T) {
	a := &fakeAuditAccessor{
		status: audit.Status{
			Enabled:            true,
			DelaySlots:         4,
			LastAuditedSlot:    123,
			RetentionSlots:     64,
			RPCFallbackEnabled: true,
		},
	}
	e := &Exporter{logger: zap.NewNop(), audit: a}

	req := httptest.NewRequest(http.MethodGet, "/v1/exporter/auditor/status", nil)
	rec := httptest.NewRecorder()
	require.NoError(t, e.AuditorStatus(rec, req))

	var resp AuditorStatusResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.True(t, resp.Data.Enabled)
	require.Equal(t, uint64(123), resp.Data.LastAuditedSlot)
}

func TestAuditorSummary_GroupsByReasonAndRole(t *testing.T) {
	rAtt := audit.RoleAttester
	rSync := audit.RoleSyncCommittee

	a := &fakeAuditAccessor{
		status: audit.Status{Enabled: true, LastAuditedSlot: 500},
		findings: []*audit.Finding{
			{Reason: audit.ReasonScheduleMissingIndex, Role: &rAtt},
			{Reason: audit.ReasonScheduleMissingIndex, Role: &rAtt},
			{Reason: audit.ReasonUnexpectedWireTrace, Role: &rSync},
		},
	}
	e := &Exporter{logger: zap.NewNop(), audit: a}

	req := httptest.NewRequest(http.MethodGet, "/v1/exporter/auditor/summary?lastN=10", nil)
	rec := httptest.NewRecorder()
	require.NoError(t, e.AuditorSummary(rec, req))

	var resp AuditorSummaryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Data)
	require.Equal(t, "SCHEDULE_MISSING_INDEX", resp.Data[0].Reason)
	require.Equal(t, uint64(2), resp.Data[0].Total)
	require.Equal(t, uint64(2), resp.Data[0].RoleCounts[string(rAtt)])
}
