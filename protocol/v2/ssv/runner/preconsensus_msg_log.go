package runner

import (
	"sort"

	"go.uber.org/zap"
)

type preConsensusMsgLogStats struct {
	totalMessages uint64
	messagesBySig map[uint64]uint64
	summaryLogged bool
}

type preConsensusSignerMsgCount struct {
	Signer   uint64 `json:"signer"`
	Messages uint64 `json:"messages"`
}

func (b *BaseRunner) resetPreConsensusMsgLog() {
	b.preConsensusMsgLogMu.Lock()
	defer b.preConsensusMsgLogMu.Unlock()

	b.preConsensusMsgLog.totalMessages = 0
	b.preConsensusMsgLog.messagesBySig = make(map[uint64]uint64)
	b.preConsensusMsgLog.summaryLogged = false
}

func (b *BaseRunner) observePreConsensusMsg(signer uint64) {
	b.preConsensusMsgLogMu.Lock()
	defer b.preConsensusMsgLogMu.Unlock()

	if b.preConsensusMsgLog.messagesBySig == nil {
		b.preConsensusMsgLog.messagesBySig = make(map[uint64]uint64)
	}

	b.preConsensusMsgLog.totalMessages++
	b.preConsensusMsgLog.messagesBySig[signer]++
}

func (b *BaseRunner) preConsensusMsgLogSummaryFieldsOnce() []zap.Field {
	b.preConsensusMsgLogMu.Lock()
	defer b.preConsensusMsgLogMu.Unlock()

	if b.preConsensusMsgLog.summaryLogged {
		return nil
	}
	b.preConsensusMsgLog.summaryLogged = true

	if len(b.preConsensusMsgLog.messagesBySig) == 0 {
		return []zap.Field{
			zap.Uint64("pre_consensus_msgs_total", b.preConsensusMsgLog.totalMessages),
			zap.Uint64("pre_consensus_msgs_unique_signers", 0),
		}
	}

	signers := make([]preConsensusSignerMsgCount, 0, len(b.preConsensusMsgLog.messagesBySig))
	for signer, count := range b.preConsensusMsgLog.messagesBySig {
		signers = append(signers, preConsensusSignerMsgCount{
			Signer:   signer,
			Messages: count,
		})
	}
	sort.Slice(signers, func(i, j int) bool { return signers[i].Signer < signers[j].Signer })

	return []zap.Field{
		zap.Uint64("pre_consensus_msgs_total", b.preConsensusMsgLog.totalMessages),
		zap.Uint64("pre_consensus_msgs_unique_signers", uint64(len(b.preConsensusMsgLog.messagesBySig))),
		zap.Any("pre_consensus_msgs_by_signer", signers),
	}
}
