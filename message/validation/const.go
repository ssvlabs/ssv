package validation

import (
	"time"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
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

// proposerPreferencesEarlyEpochs is the proposer-lookahead span in epochs (the current epoch plus
// MIN_SEED_LOOKAHEAD=1): preferences are broadcast up to this far ahead of their proposal slot. It
// bounds both how early such a message may arrive and how many slots of per-signer state to retain.
const proposerPreferencesEarlyEpochs = 2

// maxProposerPreferencesDistinctRoots bounds the distinct ProposerPreferences signing roots one
// (slot, signer) may contribute (SIP #94 §5). Unlike other pre-consensus messages (capped at 1), a
// proposer re-emits its preference under a new root when the proposal slot's dependent_root changes, so
// the bound admits a few genuine reorg-driven refreshes while still capping duplicates and flooding.
// The value is shared with the §5 dispatcher's pending stash, hence the central constant.
const maxProposerPreferencesDistinctRoots = gloas.MaxProposerPreferencesDistinctRoots

// maxRequestAuthDistinctRoots bounds the distinct RequestAuthV1 signing roots one (slot, signer)
// may contribute (issue #2962): one root per configured direct-builder entry — auth roots don't
// depend on dependent_root, so unlike §5 preferences a reorg never mints new ones — plus headroom
// for a config change between emissions. Shared with the config entry cap and the dispatcher stash.
const maxRequestAuthDistinctRoots = gloas.MaxRequestAuthDistinctRoots

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
