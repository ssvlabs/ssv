package validation

import (
	"time"
)

const (
	// lateMessageMargin is the duration past a message's TTL in which it is still considered valid.
	lateMessageMargin = time.Second * 3

	// clockErrorTolerance is the maximum amount of clock error we expect to see between nodes.
	clockErrorTolerance = time.Millisecond * 50

	maxConsensusMsgSize                      = 8388608 // TODO: calculate new value
	maxPartialSignatureMsgSize               = 1952
	maxPayloadSize                           = maxConsensusMsgSize
	maxSignedMsgSize                         = 4 + 56 + maxPayloadSize                // Max possible MsgType + MsgID + Data
	maxEncodedMsgSize                        = maxSignedMsgSize + maxSignedMsgSize/10 // 10% for encoding overhead
	allowedRoundsInFuture                    = 1
	allowedRoundsInPast                      = 2
	lateSlotAllowance                        = 2
	rsaSignatureSize                         = 256
	blsSignatureSize                         = 96
	syncCommitteeSize                        = 512
	maxSignaturesInSyncCommitteeContribution = 13
)
