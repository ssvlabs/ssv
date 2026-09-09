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

// earlyMessageMargin is how far before its slot a message may arrive, on top of clockErrorTolerance —
// the early-side counterpart of lateMessageMargin. Accepting early costs nothing: the duty queues hold
// the message until the local duty for that slot starts, and a runner still on the previous slot hands
// it back as retryable. Dropping early is fatal for the duties that open with a single-shot
// pre-consensus round — proposer RANDAO, selection proofs, registration, exit — since a dropped partial
// is never re-sent: a transient clock error just past clockErrorTolerance on the sender's slot tick
// would cost the whole duty, for the proposer the block (issue #3026). For the monotonic-slot roles
// (monotonicSlotRole) the margin has one side effect: a signer's message for slot N+1 accepted this early
// advances its slot, so its remaining slot-N messages are dropped as already advanced
// (ErrSlotAlreadyAdvanced) that much sooner — a straggler behind the signer's own later message, which
// sequential sending rules out barring network reordering.
const earlyMessageMargin = time.Second

// proposerPreferencesEarlyEpochs is the proposer-lookahead span in epochs (the current epoch plus
// MIN_SEED_LOOKAHEAD=1): preferences are broadcast up to this far ahead of their proposal slot. It
// bounds both how early such a message may arrive and how many slots of per-signer state to retain.
const proposerPreferencesEarlyEpochs = 2

// maxProposerPreferencesDistinctRoots bounds the distinct ProposerPreferences signing roots one
// (slot, signer) may contribute (SIP #94 §5): unlike other pre-consensus messages (capped at 1), a
// proposer re-emits under a new root when the slot's dependent_root changes. Derivation at the
// shared constant.
const maxProposerPreferencesDistinctRoots = gloas.MaxProposerPreferencesDistinctRoots

// maxRequestAuthDistinctRoots bounds the distinct BuilderRequestAuth signing roots one (slot, signer)
// may contribute (issue #2962): exactly one per configured direct-builder entry. Derivation at the
// shared constant.
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
