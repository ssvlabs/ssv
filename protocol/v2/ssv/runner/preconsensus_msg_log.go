package runner

import (
	"sort"
	"strconv"
	"time"

	"go.uber.org/zap"
)

type preConsensusMsgLogStats struct {
	slotStartTime time.Time
	signerAt      map[uint64]time.Duration
	summaryLogged bool
}

type preConsensusSignerTiming struct {
	Signer uint64 `json:"signer"`
	At     string `json:"at"`

	atDur time.Duration
}

func (b *BaseRunner) resetPreConsensusMsgLog(slotStartTime time.Time) {
	b.preConsensusMsgLog.slotStartTime = slotStartTime
	b.preConsensusMsgLog.signerAt = make(map[uint64]time.Duration)
	b.preConsensusMsgLog.summaryLogged = false
}

func (b *BaseRunner) observePreConsensusMsg(signer uint64) {
	now := time.Now()

	if b.preConsensusMsgLog.signerAt == nil {
		b.preConsensusMsgLog.signerAt = make(map[uint64]time.Duration)
	}

	if b.preConsensusMsgLog.slotStartTime.IsZero() {
		b.preConsensusMsgLog.slotStartTime = now
	}

	if _, seen := b.preConsensusMsgLog.signerAt[signer]; seen {
		return
	}

	b.preConsensusMsgLog.signerAt[signer] = now.Sub(b.preConsensusMsgLog.slotStartTime)
}

func (b *BaseRunner) preConsensusMsgLogSummaryFieldsOnce(quorumReachedBySigner uint64) []zap.Field {
	now := time.Now()

	if b.preConsensusMsgLog.summaryLogged {
		return nil
	}
	b.preConsensusMsgLog.summaryLogged = true

	if b.preConsensusMsgLog.slotStartTime.IsZero() {
		b.preConsensusMsgLog.slotStartTime = now
	}

	timeToQuorum := now.Sub(b.preConsensusMsgLog.slotStartTime)
	if at, ok := b.preConsensusMsgLog.signerAt[quorumReachedBySigner]; ok {
		timeToQuorum = at
	}

	signers := make([]preConsensusSignerTiming, 0, len(b.preConsensusMsgLog.signerAt))
	for signer, at := range b.preConsensusMsgLog.signerAt {
		signers = append(signers, preConsensusSignerTiming{
			Signer: signer,
			At:     signedDurationToSecondsStr(at),
			atDur:  at,
		})
	}
	sort.Slice(signers, func(i, j int) bool {
		if signers[i].atDur == signers[j].atDur {
			return signers[i].Signer < signers[j].Signer
		}
		return signers[i].atDur < signers[j].atDur
	})

	return []zap.Field{
		zap.String("time_to_quorum", durationToSecondsStr(timeToQuorum)),
		zap.Any("signer_timings", signers),
		zap.Uint64("quorum_reached_by", quorumReachedBySigner),
	}
}

func durationToSecondsStr(val time.Duration) string {
	valStr := strconv.FormatFloat(val.Seconds(), 'f', 5, 64)
	return valStr + "s"
}

func signedDurationToSecondsStr(val time.Duration) string {
	valStr := strconv.FormatFloat(val.Seconds(), 'f', 5, 64)
	if val >= 0 {
		valStr = "+" + valStr
	}
	return valStr + "s"
}
