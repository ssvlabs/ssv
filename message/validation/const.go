package validation

import (
	"time"
)

const (
	// lateMessageMargin is the duration past a message's TTL in which it is still considered valid.
	lateMessageMargin = time.Second * 3

	// clockErrorTolerance is the maximum amount of clock error we expect to see between nodes.
	clockErrorTolerance = time.Millisecond * 50

	maxConsensusMsgSize        = 705240                   // from knowledge base
	maxPartialSignatureMsgSize = 8 + 8 + 1000*(96+32+8+8) // 144016, from knowledge base
	allowedRoundsInFuture      = 1
	allowedRoundsInPast        = 2
	lateSlotAllowance          = 2
	rsaSignatureSize           = 256
	syncCommitteeSize          = 512
	operatorIDSize             = 8  // uin64
	msgTypeSize                = 8  // uint64
	msgIDSize                  = 56 // from spectypes.SSVMessage
	maxSignatures              = 13
	maxSignaturesSize          = maxSignatures * rsaSignatureSize
	maxOperatorIDSize          = maxSignatures * operatorIDSize
	maxFullDataSize            = 5243144 // from spectypes.SignedSSVMessage
)

var (
	maxPayloadDataSize = max(maxConsensusMsgSize, maxPartialSignatureMsgSize) // not const because of max TODO: const after Go 1.21
	maxSignedMsgSize   = maxSignaturesSize + maxOperatorIDSize + msgTypeSize + msgIDSize + maxPayloadDataSize + maxFullDataSize
	maxEncodedMsgSize  = maxSignedMsgSize + maxSignedMsgSize/10 // 10% for encoding overhead
)

// TODO: delete after updating to Go 1.21
func max(a, b int) int {
	if a > b {
		return a
	}

	return b
}
