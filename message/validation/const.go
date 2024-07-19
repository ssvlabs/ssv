package validation

import (
	"time"
)

const (
	// lateMessageMargin is the duration past a message's TTL in which it is still considered valid.
	lateMessageMargin = time.Second * 3
	// clockErrorTolerance is the maximum amount of clock error we expect to see between nodes.
	clockErrorTolerance   = time.Millisecond * 50
	allowedRoundsInFuture = 1
	allowedRoundsInPast   = 2
	lateSlotAllowance     = 2
	syncCommitteeSize     = 512
	rsaSignatureSize      = 256
	blsSignatureSize      = 96
	operatorIDSize        = 8 // uint64
	slotSize              = 8 // uint64
	validatorIndexSize    = 8 // uint64
	identifierSize        = 56
	rootSize              = 32
	maxSignatures         = 13
)

const (
	qbftMsgTypeSize        = 8     // uint64
	heightSize             = 8     // uint64
	roundSize              = 8     // uint64
	maxNoJustificationSize = 3616  // from KB
	max1JustificationSize  = 50624 // from KB
	maxConsensusMsgSize    = qbftMsgTypeSize + heightSize + roundSize + identifierSize + rootSize + roundSize + maxSignatures*(maxNoJustificationSize+max1JustificationSize)
)

const (
	partialSignatureSize        = 96
	partialSignatureMsgSize     = partialSignatureSize + rootSize + operatorIDSize + validatorIndexSize
	maxPartialSignatureMessages = 1000
	partialSigMsgTypeSize       = 8 // uint64
	maxPartialSignatureMsgsSize = partialSigMsgTypeSize + slotSize + maxPartialSignatureMessages*partialSignatureMsgSize
)

const (
	msgTypeSize       = 8 // uint64
	maxSignaturesSize = maxSignatures * rsaSignatureSize
	maxOperatorIDSize = maxSignatures * operatorIDSize
	maxFullDataSize   = 5243144 // from spectypes.SignedSSVMessage
)

var (
	maxPayloadDataSize = max(maxConsensusMsgSize, maxPartialSignatureMsgsSize) // not const because of max TODO: const after Go 1.21
	maxSignedMsgSize   = maxSignaturesSize + maxOperatorIDSize + msgTypeSize + identifierSize + maxPayloadDataSize + maxFullDataSize
	maxEncodedMsgSize  = maxSignedMsgSize + maxSignedMsgSize/10 // 10% for encoding overhead
)

// TODO: delete after updating to Go 1.21
func max(a, b int) int {
	if a > b {
		return a
	}

	return b
}
