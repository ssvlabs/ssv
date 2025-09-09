package exporter

import (
	"encoding/hex"
	"time"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/exporter"
)

type committeeRequest struct {
	From         uint64       `json:"from"`
	To           uint64       `json:"to"`
	CommitteeIDs api.HexSlice `json:"committeeIDs"`
}

func (req *committeeRequest) parseCommitteeIds() []spectypes.CommitteeID {
	committeeIDs := make([]spectypes.CommitteeID, len(req.CommitteeIDs))
	for i, cmt := range req.CommitteeIDs {
		copy(committeeIDs[i][:], cmt)
	}
	return committeeIDs
}

type committeeTraceResponse struct {
	Data   []committeeTrace `json:"data"`
	Errors []string         `json:"errors,omitempty"`
}

type committeeTrace struct {
	Slot      uint64    `json:"slot"`
	Consensus []round   `json:"consensus"`
	Decideds  []decided `json:"decideds"`

	SyncCommittee []committeeMessage `json:"sync_committee"`
	Attester      []committeeMessage `json:"attester"`

	CommitteeID string `json:"committeeID"`
	Proposal    string `json:"proposalData,omitempty"`
}

type committeeMessage struct {
	Signer       uint64    `json:"signer"`
	ValidatorIdx []uint64  `json:"validatorIdx"`
	ReceivedTime time.Time `json:"time"`
}

func toCommitteeTrace(t *exporter.CommitteeDutyTrace) committeeTrace {
	return committeeTrace{
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

func toCommitteePost(m []*exporter.SignerData) (out []committeeMessage) {
	for _, mt := range m {
		out = append(out, committeeMessage{
			Signer:       mt.Signer,
			ValidatorIdx: toUint64Slice(mt.ValidatorIdx),
			ReceivedTime: toTime(mt.ReceivedTime),
		})
	}
	return
}
