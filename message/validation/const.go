package validation

import (
	"time"
)

const (
	// lateMessageMargin is the duration past a message's TTL in which it is still considered valid.
	lateMessageMargin = time.Second * 3

	// clockErrorTolerance is the maximum amount of clock error we expect to see between nodes.
	clockErrorTolerance = time.Millisecond * 50

	// TODO: make sure values are correct
	maxConsensusMsgSize                      = 6291829 // TODO: 8+8+8+56+32+8+13*65536+13*65536 instead?
	maxPartialSignatureMsgSize               = 8 + 8 + 1000*(96+32+8+8)
	maxPayloadSize                           = maxConsensusMsgSize                               // max(maxConsensusMsgSize, maxPartialSignatureMsgSize)
	maxSignedMsgSize                         = 13*256 + 13*8 + 8 + 56 + maxPayloadSize + 5243144 // Max possible MsgType + MsgID + Data
	maxEncodedMsgSize                        = maxSignedMsgSize + maxSignedMsgSize/10            // 10% for encoding overhead
	allowedRoundsInFuture                    = 1
	allowedRoundsInPast                      = 2
	lateSlotAllowance                        = 2
	rsaSignatureSize                         = 256
	syncCommitteeSize                        = 512
	maxSignaturesInSyncCommitteeContribution = 13
)
