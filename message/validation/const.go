package validation

import (
	"time"
)

const (
	// lateMessageMargin is the duration past a message's TTL in which it is still considered valid.
	lateMessageMargin = time.Second * 3

	// clockErrorTolerance is the maximum amount of clock error we expect to see between nodes.
	clockErrorTolerance = time.Millisecond * 50

	maxConsensusMsgSize                      = 705240
	maxPartialSignatureMsgSize               = 8 + 8 + 1000*(96+32+8+8) // 144016
	allowedRoundsInFuture                    = 1
	allowedRoundsInPast                      = 2
	lateSlotAllowance                        = 2
	rsaSignatureSize                         = 256
	syncCommitteeSize                        = 512
	maxSignaturesInSyncCommitteeContribution = 13
)

var (
	maxPayloadSize    = max(maxConsensusMsgSize, maxPartialSignatureMsgSize) // not const because of max TODO: const after Go 1.21
	maxSignedMsgSize  = 13*256 + 13*8 + 8 + 56 + maxPayloadSize + 5243144
	maxEncodedMsgSize = maxSignedMsgSize + maxSignedMsgSize/10 // 10% for encoding overhead
)

// TODO: delete after updating to Go 1.21
func max(a, b int) int {
	if a > b {
		return a
	}

	return b
}
