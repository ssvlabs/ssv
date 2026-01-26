package validation

import (
	"time"

	"github.com/ssvlabs/ssv-spec/types/spectest/tests/maxmsgsize"
)

// To add some encoding overhead for ssz, we use (N + N/encodingOverheadDivisor + 4) for a structure with expected size N

const (
	// lateMessageMargin is the duration past a message's TTL in which it is still considered valid.
	lateMessageMargin = time.Second * 3
	// clockErrorTolerance is the maximum amount of clock error we expect to see between nodes.
	clockErrorTolerance   = time.Millisecond * 50
	allowedRoundsInFuture = 1
	allowedRoundsInPast   = 2
	LateSlotAllowance     = 2
	rsaSignatureSize      = 256
	operatorIDSize        = 8 // uint64
	maxSignatures         = 13
	signatureSize         = 256
	signatureOffset       = 0
	operatorIDOffset      = signatureOffset + signatureSize
	MessageOffset         = operatorIDOffset + operatorIDSize
)

const (
	MaxEncodedMsgSize              = maxmsgsize.MaxSizeSignedSSVMessageFromQBFTWith2Justification
	maxEncodedConsensusMsgSize     = maxmsgsize.MaxSizeSSVMessageFromQBFTMessage
	maxEncodedPartialSignatureSize = maxmsgsize.MaxSizeSSVMessageFromPartialSignatureMessages
	maxPayloadDataSize             = max(maxEncodedConsensusMsgSize, maxEncodedPartialSignatureSize)
)
