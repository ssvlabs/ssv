package validation

import (
	"time"
)

// To add some encoding overhead for ssz, we use (N + N/encodingOverheadDivisor + 4) for a structure with expected size N

const (
	// lateMessageMargin is the duration past a message's TTL in which it is still considered valid.
	lateMessageMargin = time.Second * 3
	// clockErrorTolerance is the maximum amount of clock error we expect to see between nodes.
	clockErrorTolerance     = time.Millisecond * 50
	allowedRoundsInPast     = 2
	allowedRoundsInFuture   = 1
	LateSlotAllowance       = 2
	rsaSignatureSize        = 256
	operatorIDSize          = 8 // uint64
	slotSize                = 8 // uint64
	validatorIndexSize      = 8 // uint64
	identifierSize          = 56
	rootSize                = 32
	maxSignatures           = 13
	encodingOverheadDivisor = 20 // Divisor for message size to get encoding overhead, e.g. 10 for 10%, 20 for 5%. Done this way to keep const int.
)

const (
	signatureSize    = 256
	signatureOffset  = 0
	operatorIDOffset = signatureOffset + signatureSize
	MessageOffset    = operatorIDOffset + operatorIDSize
)

const (
	qbftMsgTypeSize            = 8     // uint64
	heightSize                 = 8     // uint64
	roundSize                  = 8     // uint64
	maxNoJustificationSize     = 3616  // from KB
	max1JustificationSize      = 50624 // from KB
	maxConsensusMsgSize        = qbftMsgTypeSize + heightSize + roundSize + identifierSize + rootSize + roundSize + maxSignatures*(maxNoJustificationSize+max1JustificationSize)
	maxEncodedConsensusMsgSize = maxConsensusMsgSize + maxConsensusMsgSize/encodingOverheadDivisor + 4
)

const (
	partialSignatureSize    = 96
	partialSignatureMsgSize = partialSignatureSize + rootSize + operatorIDSize + validatorIndexSize
	// maxPartialSignatureMessages is the post-fork worst case (boole RoleAggregatorCommittee). The
	// count derives from the inner ssv-spec types/spectest/tests/maxmsgsize.MaxSizePartialSignatureMessages;
	// the drift guard in const_test.go checks the full envelope, MaxSizeSSVMessageFromPartialSignatureMessages.
	maxPartialSignatureMessages    = 5048
	partialSigMsgTypeSize          = 8 // uint64
	maxPartialSignatureMsgsSize    = partialSigMsgTypeSize + slotSize + maxPartialSignatureMessages*partialSignatureMsgSize
	maxEncodedPartialSignatureSize = maxPartialSignatureMsgsSize + maxPartialSignatureMsgsSize/encodingOverheadDivisor + 4

	// preForkMaxPartialSignatureMessages is the pre-boole worst case (RoleCommittee,
	// min(2*V, V+SYNC_COMMITTEE_SIZE) with the spec's V=1000 bound), matching pre-boole
	// ssv-spec v1.2.2 maxmsgsize.maxSizePartialSignatureMessages (1512 messages, 217748
	// bytes). Deliberately above the 1000 enforced before the boole convergence, which
	// sat slightly below the spec's structural bound. The pinned (post-fork) spec no
	// longer publishes this constant, so const_test.go guards it against the hardcoded
	// v1.2.2 value instead.
	preForkMaxPartialSignatureMessages    = 1512
	preForkMaxPartialSignatureMsgsSize    = partialSigMsgTypeSize + slotSize + preForkMaxPartialSignatureMessages*partialSignatureMsgSize
	preForkMaxEncodedPartialSignatureSize = preForkMaxPartialSignatureMsgsSize + preForkMaxPartialSignatureMsgsSize/encodingOverheadDivisor + 4
)

const (
	msgTypeSize           = 8 // uint64
	maxSignaturesSize     = maxSignatures * rsaSignatureSize
	maxOperatorIDSize     = maxSignatures * operatorIDSize
	pectraMaxFullDataSize = 8388836 // from spectypes.SignedSSVMessage
)

const (
	maxPayloadDataSize = max(maxEncodedConsensusMsgSize, maxEncodedPartialSignatureSize)
	maxSignedMsgSize   = maxSignaturesSize + maxOperatorIDSize + msgTypeSize + identifierSize + maxPayloadDataSize + pectraMaxFullDataSize
)

// MaxEncodedMsgSize defines max pubsub message size
const MaxEncodedMsgSize = maxSignedMsgSize + maxSignedMsgSize/encodingOverheadDivisor + 4
