package exporter

import (
	"encoding/hex"
	"time"

	"github.com/hashicorp/go-multierror"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/exporter"
	exporter2 "github.com/ssvlabs/ssv/exporter2"
)

type CommitteeIDLengthError struct {
	CommitteeID api.Hex
}

// CommitteeTracesRequest represents the filter parameters accepted by the
// committee traces endpoints.
type CommitteeTracesRequest struct {
	// From is the starting slot (inclusive).
	From uint64 `json:"from" format:"int64" minimum:"0"`
	// To is the ending slot (inclusive).
	To uint64 `json:"to" format:"int64" minimum:"0"`
	// CommitteeIDs is a comma-separated list of committee IDs (hex, 64 chars per ID).
	CommitteeIDs api.HexSlice `json:"committeeIDs" swaggertype:"array,string" format:"hex" minLength:"64" maxLength:"64" pattern:"^[0-9a-f]{64}$"`
}

func (req *CommitteeTracesRequest) parseCommitteeIds() []spectypes.CommitteeID {
	committeeIDs := make([]spectypes.CommitteeID, len(req.CommitteeIDs))
	for i, cmt := range req.CommitteeIDs {
		copy(committeeIDs[i][:], cmt)
	}
	return committeeIDs
}

func toCommitteeTracesQuery(r *CommitteeTracesRequest) (*exporter2.CommitteeTracesQuery, *CommitteeIDLengthError) {
	requiredLength := len(spectypes.CommitteeID{})
	for _, cmt := range r.CommitteeIDs {
		if len(cmt) != requiredLength {
			return nil, &CommitteeIDLengthError{CommitteeID: cmt}
		}
	}

	q := &exporter2.CommitteeTracesQuery{
		From:         r.From,
		To:           r.To,
		CommitteeIDs: r.parseCommitteeIds(),
	}

	return q, nil
}

// CommitteeTracesResponse represents the API response returned by the
// committee traces endpoints.
type CommitteeTracesResponse struct {
	// Data contains the list of committee duty traces matching the request.
	Data []CommitteeTrace `json:"data"`
	// Schedule lists requested duties unioned at the committee-level by role.
	Schedule []CommitteeSchedule `json:"schedule"`
	// Errors lists non-fatal issues encountered while building the response (duties not found, enrichment errors, etc.).
	Errors []string `json:"errors,omitempty" swaggertype:"array,string" example:"committee duty missing for slot 123456"`
}

func toCommitteeTraceResponse(result *exporter2.CommitteeTracesResult, errs *multierror.Error) *CommitteeTracesResponse {
	r := new(CommitteeTracesResponse)
	r.Data = make([]CommitteeTrace, 0)
	for _, duty := range result.Traces {
		r.Data = append(r.Data, toCommitteeTrace(duty))
	}
	r.Schedule = toCommitteeSchedule(result.Schedule)
	r.Errors = toStrings(errs)
	return r
}

// CommitteeTrace contains the duty trace information for a specific committee.
type CommitteeTrace struct {
	// Slot is the duty slot for this committee trace.
	Slot uint64 `json:"slot" format:"int64"`
	// Consensus lists per-round QBFT messages observed for this committee.
	Consensus []Round `json:"consensus"`
	// Decideds lists decided messages emitted for this duty.
	Decideds []Decided `json:"decideds"`

	// SyncCommittee contains post-consensus messages for sync-committee duties.
	SyncCommittee []CommitteeMessage `json:"sync_committee"`
	// Attester contains post-consensus messages for attester duties.
	Attester []CommitteeMessage `json:"attester"`

	// CommitteeID is the 32-byte committee identifier (hex).
	CommitteeID string `json:"committeeID" format:"hex"`
	// Proposal is the hex-encoded proposal payload for this duty, if available.
	Proposal string `json:"proposalData,omitempty" format:"hex"`
}

// CommitteeMessage encapsulates post-consensus committee data.
type CommitteeMessage struct {
	// Signer is the operator ID that produced the message.
	Signer uint64 `json:"signer"`
	// ValidatorIdx lists related validator indices, when applicable.
	ValidatorIdx []uint64 `json:"validatorIdx"`
	// ReceivedTime is when the message was observed.
	ReceivedTime time.Time `json:"time" format:"date-time"`
}

func toCommitteeTrace(t *exporter.CommitteeDutyTrace) CommitteeTrace {
	return CommitteeTrace{
		// consensus trace
		Slot:          uint64(t.Slot),
		Consensus:     toRounds(t.Rounds),
		Decideds:      toDecideds(t.Decideds),
		SyncCommittee: toCommitteePost(t.SyncCommittee),
		Attester:      toCommitteePost(t.Attester),
		CommitteeID:   hex.EncodeToString(t.CommitteeID[:]),
		Proposal:      formatProposalData(t.ProposalData),
	}
}

func toCommitteePost(m []*exporter.SignerData) (out []CommitteeMessage) {
	for _, mt := range m {
		out = append(out, CommitteeMessage{
			Signer:       mt.Signer,
			ValidatorIdx: toUint64Slice(mt.ValidatorIdx),
			ReceivedTime: toTime(mt.ReceivedTime),
		})
	}
	return
}

// CommitteeSchedule presents per-committee scheduled roles as role->indices for a slot.
type CommitteeSchedule struct {
	Slot        uint64              `json:"slot" format:"int64"`
	CommitteeID string              `json:"committeeID" format:"hex"`
	Roles       map[string][]uint64 `json:"roles"`
}

func toCommitteeSchedule(entries []exporter2.CommitteeScheduleEntry) []CommitteeSchedule {
	out := make([]CommitteeSchedule, 0, len(entries))
	for _, e := range entries {
		roles := make(map[string][]uint64, len(e.Roles))
		for role, idxs := range e.Roles {
			indices := make([]uint64, 0, len(idxs))
			for _, idx := range idxs {
				indices = append(indices, uint64(idx))
			}
			roles[role.String()] = indices
		}
		out = append(out, CommitteeSchedule{
			Slot:        uint64(e.Slot),
			CommitteeID: hex.EncodeToString(e.CommitteeID[:]),
			Roles:       roles,
		})
	}
	return out
}
