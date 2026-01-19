package runner

import (
	"sort"
	"strconv"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"
)

type preConsensusMsgLogStats struct {
	slotStartTime      time.Time
	signerTimeIntoSlot map[uint64]time.Duration
}

func (b *BaseRunner) resetPreConsensusLogSummary(nextSlot phase0.Slot) {
	summary := &preConsensusMsgLogStats{
		slotStartTime:      time.Time{},
		signerTimeIntoSlot: make(map[uint64]time.Duration),
	}

	// b.NetworkConfig can only be nil in tests, this is fast & dirty work-around for it.
	if b.NetworkConfig != nil {
		summary.slotStartTime = b.NetworkConfig.SlotStartTime(nextSlot)
	}

	b.preConsensusMsgLog.Store(summary)
}

func (b *BaseRunner) observePreConsensusMsg(signer uint64) {
	// Duplicate message from the same signer should never arrive here, but handle it just in case.
	if _, seen := b.preConsensusMsgLog.Load().signerTimeIntoSlot[signer]; seen {
		return
	}

	// b.preConsensusMsgLog.Load().signerTimeIntoSlot can only be nil in tests, this is fast & dirty work-around for it.
	if b.preConsensusMsgLog.Load().signerTimeIntoSlot == nil {
		b.preConsensusMsgLog.Load().signerTimeIntoSlot = make(map[uint64]time.Duration)
	}

	b.preConsensusMsgLog.Load().signerTimeIntoSlot[signer] = time.Since(b.preConsensusMsgLog.Load().slotStartTime)
}

func (b *BaseRunner) preConsensusMsgLogSummaryFields(quorumReachedBySigner uint64) []zap.Field {
	timeToQuorum := time.Since(b.preConsensusMsgLog.Load().slotStartTime)
	if at, ok := b.preConsensusMsgLog.Load().signerTimeIntoSlot[quorumReachedBySigner]; ok {
		timeToQuorum = at
	}

	type preConsensusSignerTiming struct {
		Signer       uint64 `json:"signer"`
		TimeIntoSlot string `json:"time_into_slot"`

		atDur time.Duration
	}
	signerTimings := make([]preConsensusSignerTiming, 0, len(b.preConsensusMsgLog.Load().signerTimeIntoSlot))
	for signer, at := range b.preConsensusMsgLog.Load().signerTimeIntoSlot {
		signerTimings = append(signerTimings, preConsensusSignerTiming{
			Signer:       signer,
			TimeIntoSlot: signedDurationToSecondsStr(at),
			atDur:        at,
		})
	}
	sort.Slice(signerTimings, func(i, j int) bool {
		if signerTimings[i].atDur == signerTimings[j].atDur {
			return signerTimings[i].Signer < signerTimings[j].Signer
		}
		return signerTimings[i].atDur < signerTimings[j].atDur
	})

	return []zap.Field{
		zap.String("time_to_quorum", durationToSecondsStr(timeToQuorum)),
		zap.Any("signer_timings", signerTimings),
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
