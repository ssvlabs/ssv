package validation

// partial_validation.go contains methods for validating partial signature messages

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/libp2p/go-libp2p/core/peer"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

func (mv *messageValidator) validatePartialSignatureMessage(
	ctx context.Context,
	signedSSVMessage *spectypes.SignedSSVMessage,
	committeeInfo CommitteeInfo,
	topic string,
	receivedFrom peer.ID,
	receivedAt time.Time,
) (
	*spectypes.PartialSignatureMessages,
	error,
) {
	ssvMessage := signedSSVMessage.SSVMessage

	if len(ssvMessage.Data) > maxEncodedPartialSignatureSize {
		e := ErrSSVDataTooBig
		e.got = len(ssvMessage.Data)
		e.want = maxEncodedPartialSignatureSize
		return nil, e
	}

	partialSignatureMessages := &spectypes.PartialSignatureMessages{}
	if err := partialSignatureMessages.Decode(ssvMessage.Data); err != nil {
		e := ErrUndecodableMessageData
		e.innerErr = err
		return nil, e
	}

	if err := mv.validateTopicAtSlot(committeeInfo, topic, partialSignatureMessages.Slot); err != nil {
		return partialSignatureMessages, err
	}
	if err := mv.validateDomainAtSlot(ssvMessage.GetID(), partialSignatureMessages.Slot); err != nil {
		return partialSignatureMessages, err
	}

	if err := mv.validatePartialSignatureMessageSemantics(signedSSVMessage, partialSignatureMessages, committeeInfo.validatorIndices); err != nil {
		return partialSignatureMessages, err
	}

	state := mv.validatorState(ssvMessage.GetID(), committeeInfo)
	if err := mv.validatePartialSigMessagesByDutyLogic(signedSSVMessage, partialSignatureMessages, committeeInfo, receivedFrom, receivedAt, state); err != nil {
		return partialSignatureMessages, err
	}

	if err := ctx.Err(); err != nil {
		return partialSignatureMessages, err
	}

	signature := signedSSVMessage.Signatures[0]
	signer := signedSSVMessage.OperatorIDs[0]
	if err := mv.signatureVerifier.VerifySignature(signer, ssvMessage, signature); err != nil {
		e := ErrSignatureVerification
		e.innerErr = fmt.Errorf("verify opid: %v signature: %w", signer, err)
		return partialSignatureMessages, e
	}

	if err := mv.updatePartialSignatureState(partialSignatureMessages, receivedFrom, state, signer, committeeInfo); err != nil {
		return partialSignatureMessages, err
	}

	return partialSignatureMessages, nil
}

func (mv *messageValidator) validatePartialSignatureMessageSemantics(
	signedSSVMessage *spectypes.SignedSSVMessage,
	partialSignatureMessages *spectypes.PartialSignatureMessages,
	validatorIndices []phase0.ValidatorIndex,
) error {
	role := signedSSVMessage.SSVMessage.GetID().GetRoleType()
	slot := partialSignatureMessages.Slot

	// Rule: If role is invalid
	if !mv.validRoleAtSlot(role, slot) {
		e := ErrInvalidRole
		e.got = fmt.Sprintf("%v (%d) @ slot %v", role, role, slot)
		return e
	}

	// Rule: Partial Signature message must have 1 signer
	signers := signedSSVMessage.OperatorIDs
	if len(signers) != 1 {
		return ErrPartialSigMessageMustHaveOneSigner
	}

	signer := signers[0]

	// Rule: Partial signature message must not have full data
	if len(signedSSVMessage.FullData) > 0 {
		return ErrFullDataNotInConsensusMessage
	}

	// Rule: Valid signature type
	if !mv.validPartialSigMsgType(partialSignatureMessages.Type) {
		e := ErrInvalidPartialSignatureType
		e.got = partialSignatureMessages.Type
		return e
	}

	// Rule: Partial signature type must match expected type:
	// - PostConsensusPartialSig, for Committee duty
	// - RandaoPartialSig or PostConsensusPartialSig for Proposer
	// - SelectionProofPartialSig or PostConsensusPartialSig for Aggregator
	// - SelectionProofPartialSig or PostConsensusPartialSig for Sync committee contribution
	// - ValidatorRegistrationPartialSig for Validator Registration
	// - VoluntaryExitPartialSig for Voluntary Exit
	// - PTCAttesterPartialSig for PTC attestation
	// - ProposerPreferencesPartialSig or RequestAuthPartialSig for Proposer Preferences
	if !mv.partialSignatureTypeMatchesRole(partialSignatureMessages.Type, role) {
		return ErrPartialSignatureTypeRoleMismatch
	}

	// Rule: Partial signature message must have at least one signature
	if len(partialSignatureMessages.Messages) == 0 {
		return ErrNoMessagesInPartialSigMessage
	}

	// Rule: a validator-role packet carries at most maxValidatorRoleSignatures entries (committee and
	// contribution packets are bounded in validatePartialSigMessagesByDutyLogic, which knows the committee's
	// size). Checked with the structural rules, so an over-long packet is rejected before a duty-logic budget
	// could ignore it.
	if !mv.committeeRole(role) && role != ssvtypes.RoleSyncCommitteeContribution {
		if count, limit := len(partialSignatureMessages.Messages), mv.maxValidatorRoleSignatures(role, partialSignatureMessages.Type, slot); count > limit {
			e := ErrTooManySignaturesInPartialSigMessage
			e.got = count
			e.want = limit
			return e
		}
	}

	firstValidatorIndex := partialSignatureMessages.Messages[0].ValidatorIndex
	for _, message := range partialSignatureMessages.Messages {
		// Rule: Partial signature must have expected length. Already enforced by ssz.

		// Rule: Partial signature signer must be consistent
		if message.Signer != signer {
			e := ErrInconsistentSigners
			e.got = signer
			e.want = message.Signer
			return e
		}

		if !mv.committeeRole(role) {
			// Rule: (only for Validator duties) every entry carries the same validator index — SIP #94 §7
			// for the Gloas proposer's two-entry packet, REJECT otherwise. Unlike the membership check
			// below, this needs no knowledge of the validator set: a mismatch is a malformed packet.
			if message.ValidatorIndex != firstValidatorIndex {
				e := ErrInconsistentValidatorIndex
				e.got = message.ValidatorIndex
				e.want = firstValidatorIndex
				return e
			}

			// Rule: (only for Validator duties) Validator index must match with validatorPK
			// For Committee duties, we don't assume that operators are synced on the validators set
			// So, we can't make this assertion
			// Deliberate relaxation — rationale and blast radius: ssvlabs/knowledge-base#2
			if !slices.Contains(validatorIndices, message.ValidatorIndex) {
				e := ErrValidatorIndexMismatch
				e.got = message.ValidatorIndex
				e.want = validatorIndices
				return e
			}
		}
	}

	return nil
}

func (mv *messageValidator) validatePartialSigMessagesByDutyLogic(
	signedSSVMessage *spectypes.SignedSSVMessage,
	partialSignatureMessages *spectypes.PartialSignatureMessages,
	committeeInfo CommitteeInfo,
	receivedFrom peer.ID,
	receivedAt time.Time,
	state *ValidatorState,
) error {
	role := signedSSVMessage.SSVMessage.GetID().GetRoleType()
	messageSlot := partialSignatureMessages.Slot
	signer := signedSSVMessage.OperatorIDs[0]

	// Rule: validator registrations end at the Gloas fork (SIP #94 §5). One with a Gloas slot is rejected by
	// validRoleAtSlot; from the epoch after the fork, one with any slot can only be a replay, as registrations
	// have no lateness limit. IGNORE, not REJECT: the condition comes from the local clock, not the message.
	if role == spectypes.RoleValidatorRegistration && mv.registrationsRetired(receivedAt) {
		return ErrValidatorRegistrationRetired
	}

	// The signature is verified after these checks, so read the operator's state without allocating it:
	// updatePartialSignatureState allocates once the message is verified.
	operatorState := state.peekOperatorState(committeeInfo.signerIndex(signer))

	// Rule: Height must not be "old" — a monotonic-slot signer must not regress to an earlier slot
	// once it has advanced (see monotonicSlotRole for the exemptions).
	if mv.monotonicSlotRole(role) {
		maxSlot := operatorState.MaxSlot()
		if maxSlot != 0 && maxSlot > partialSignatureMessages.Slot {
			e := ErrSlotAlreadyAdvanced
			e.got = partialSignatureMessages.Slot
			e.want = maxSlot
			return e
		}
	}

	randaoMsg := partialSignatureMessages.Type == spectypes.RandaoPartialSig
	if err := mv.validateBeaconDuty(signedSSVMessage.SSVMessage.GetID().GetRoleType(), messageSlot, committeeInfo.validatorIndices, randaoMsg); err != nil {
		return err
	}

	if signerState := operatorState.GetSignerStateForSlot(messageSlot); signerState != nil {
		// Rule: Expect to receive at most:
		// - 1 PostConsensusPartialSig, for Committee duty
		// - 1 RandaoPartialSig and 1 PostConsensusPartialSig for Proposer
		// - 1 SelectionProofPartialSig and 1 PostConsensusPartialSig for Aggregator
		// - 1 SelectionProofPartialSig and 1 PostConsensusPartialSig for Sync committee contribution
		// - 1 AggregatorCommitteePartialSig and 1 PostConsensusPartialSig for AggregatorCommittee
		// - 1 ValidatorRegistrationPartialSig for Validator Registration
		// - 1 VoluntaryExitPartialSig for Voluntary Exit
		// - 1 PTCAttesterPartialSig for PTC attestation
		// - ProposerPreferencesPartialSig and RequestAuthPartialSig for Proposer Preferences, each up to
		//   its own distinct-root budget
		if err := validatePartialSignatureMessageLimit(partialSignatureMessages, receivedFrom, signerState); err != nil {
			return err
		}
	}

	// Rule: current slot must be between duty's starting slot and:
	// - duty's starting slot + 34 (committee, aggregator, and aggregator committee)
	// - duty's starting slot + 3 (other duties)
	if err := mv.validateSlotTime(messageSlot, role, receivedAt); err != nil {
		return err
	}

	if err := mv.validateDutyCount(signedSSVMessage.SSVMessage.GetID(), messageSlot, committeeInfo.validatorIndices, operatorState); err != nil {
		return err
	}

	clusterValidatorCount := len(committeeInfo.validatorIndices)
	partialSignatureMessageCount := len(partialSignatureMessages.Messages)

	if mv.committeeRole(role) {
		scSubnets := 1
		if role == spectypes.RoleAggregatorCommittee {
			scSubnets = 4
		}

		maxDutiesForRole := scSubnets + 1

		// Rule: The number of signatures must be:
		// - <= min(2*V, V + SYNC_COMMITTEE_SIZE) for committee,
		// - <= min(5*V, V + 4*SYNC_COMMITTEE_SIZE) for aggregator committee,
		// where V is the number of validators assigned to the cluster
		// #nosec G115
		messageLimit := min(maxDutiesForRole*clusterValidatorCount, clusterValidatorCount+scSubnets*int(mv.netCfg.SyncCommitteeSize))
		if partialSignatureMessageCount > messageLimit {
			e := ErrTooManySignaturesInPartialSigMessage
			e.got = partialSignatureMessageCount
			e.want = messageLimit
			return e
		}

		// Rule: a ValidatorIndex can't appear in the []*PartialSignatureMessage list:
		// - more than 2 times for RoleCommittee
		// - more than 5 times for RoleAggregatorCommittee
		validatorIndexCount := make(map[phase0.ValidatorIndex]int)
		for _, message := range partialSignatureMessages.Messages {
			validatorIndexCount[message.ValidatorIndex]++
			if cnt := validatorIndexCount[message.ValidatorIndex]; cnt > maxDutiesForRole {
				e := ErrTooManyEqualValidatorIndicesInPartialSignatures
				e.got = cnt
				e.want = fmt.Sprintf("<=%d", maxDutiesForRole)
				return e
			}
		}
	} else if role == ssvtypes.RoleSyncCommitteeContribution {
		// Rule: The number of signatures must be <= MaxSignaturesInSyncCommitteeContribution for the sync committee contribution duty
		if partialSignatureMessageCount > maxSignatures {
			e := ErrTooManySignaturesInPartialSigMessage
			e.got = partialSignatureMessageCount
			e.want = maxSignatures
			return e
		}
	}
	// Other validator roles' entry limit is checked in validatePartialSignatureMessageSemantics.

	return nil
}

// registrationsRetired reports whether receivedAt is past the Gloas fork epoch, from which no validator
// registration is accepted. The fork epoch itself still admits those in flight from the last pre-fork slots.
func (mv *messageValidator) registrationsRetired(receivedAt time.Time) bool {
	epoch := mv.netCfg.EstimatedEpochAtSlot(mv.netCfg.EstimatedSlotAtTime(receivedAt))
	return epoch > 0 && mv.netCfg.IsGloas(epoch-1)
}

// maxValidatorRoleSignatures bounds the entries of a validator-role (non-committee, non-contribution)
// partial-signature packet (SIP #94 §7): one, with two exceptions. The proposer's post-consensus packet at
// a Gloas slot carries the block root and, on the self-build path, the §6 blinded-envelope root, so up to
// two; a request-auth packet carries one entry per auth root, up to maxRequestAuthEntries. The runner
// matches each entry to its expected roots; validation only bounds the count.
func (mv *messageValidator) maxValidatorRoleSignatures(role spectypes.RunnerRole, msgType spectypes.PartialSigMsgType, slot phase0.Slot) int {
	switch {
	case role == spectypes.RoleProposer && msgType == spectypes.PostConsensusPartialSig && mv.netCfg.IsGloasAtSlot(slot):
		return 2
	case role == spectypes.RoleProposerPreferences && msgType == spectypes.RequestAuthPartialSig:
		return maxRequestAuthEntries
	default:
		return 1
	}
}

// validatePartialSignatureMessageLimit checks if the provided partial signature message exceeds the set limits.
// Returns an error if the message type exceeds its respective count limit.
func validatePartialSignatureMessageLimit(
	m *spectypes.PartialSignatureMessages,
	receivedFrom peer.ID,
	signerState *SignerStateForSlotRound,
) error {
	peerState := signerState.peekPeer(receivedFrom)
	switch m.Type {
	case spectypes.RandaoPartialSig, ssvtypes.SelectionProofPartialSig, ssvtypes.ContributionProofs,
		spectypes.ValidatorRegistrationPartialSig, spectypes.VoluntaryExitPartialSig,
		spectypes.AggregatorCommitteePartialSig, spectypes.PTCAttesterPartialSig:
		if peerState.SeenMsgTypes.reachedPreConsensusLimit() {
			// Check if the same peer is sending us a "logical duplicate" message, reject message to punish.
			e := ErrTooManyPartialSigMessage
			e.reject = true
			e.got = fmt.Sprintf("pre-consensus, having %v", peerState.SeenMsgTypes.String())
			return e
		}
		if signerState.World.SeenMsgTypes.reachedPreConsensusLimit() {
			// Check if a different peer is sending us a "logical duplicate" message, ignore message since this
			// is expected occasionally.
			e := ErrTooManyPartialSigMessage
			e.got = fmt.Sprintf("pre-consensus, having %v", signerState.World.SeenMsgTypes.String())
			return e
		}
	case spectypes.ProposerPreferencesPartialSig:
		// SIP #94 §5: a dependent_root refresh re-emits under a new root, so the type is budgeted by
		// distinct signing root instead of the usual ≤1 pre-consensus cap.
		return validateDistinctRootBudget(m, signerState, "proposer-preferences", maxProposerPreferencesDistinctRoots)
	case spectypes.RequestAuthPartialSig:
		// SIP #94 §5: one root per configured builder, under the same budget scheme.
		return validateDistinctRootBudget(m, signerState, "request-auth", maxRequestAuthDistinctRoots)
	case spectypes.PostConsensusPartialSig:
		if peerState.SeenMsgTypes.reachedPostConsensusLimit() {
			// Check if the same peer is sending us a "logical duplicate" message, reject message to punish.
			e := ErrTooManyPartialSigMessage
			e.reject = true
			e.got = fmt.Sprintf("post-consensus, having %v", peerState.SeenMsgTypes.String())
			return e
		}
		if signerState.World.SeenMsgTypes.reachedPostConsensusLimit() {
			// Check if a different peer is sending us a "logical duplicate" message, ignore message since this
			// is expected occasionally.
			e := ErrTooManyPartialSigMessage
			e.got = fmt.Sprintf("post-consensus, having %v", signerState.World.SeenMsgTypes.String())
			return e
		}
	default:
		return fmt.Errorf("unexpected partial signature message type: %d", m.Type)
	}

	return nil
}

// validateDistinctRootBudget applies the dedup for the root-budgeted types (§5 preferences and request
// auths), per (slot, signer) and type (SIP #94 §7). A packet is IGNOREd when it adds no new root, whichever
// peer relays it — an honest sender's retry or restart repeats its roots once the gossip duplicate cache has
// expired, so a repeat proves no fault — or when its new roots would take the signer past the budget, which
// is rate-limiting, not a provable violation. A packet mixing recorded and new roots within the budget
// passes. A root repeated within the packet counts once here, though each entry counts toward the packet's
// entry limit.
func validateDistinctRootBudget(
	m *spectypes.PartialSignatureMessages,
	signerState *SignerStateForSlotRound,
	label string,
	budget int,
) error {
	seen := seenRootsFor(signerState, m.Type)
	fresh := make(map[[32]byte]struct{}, len(m.Messages))
	for _, msg := range m.Messages {
		if !seen.has(msg.SigningRoot) {
			fresh[msg.SigningRoot] = struct{}{}
		}
	}
	if len(fresh) == 0 {
		e := ErrTooManyPartialSigMessage
		e.got = label + ", no new signing root"
		return e
	}
	if len(*seen)+len(fresh) > budget {
		e := ErrTooManyPartialSigMessage
		e.got = fmt.Sprintf("%s, %d distinct root(s) seen and %d new", label, len(*seen), len(fresh))
		return e
	}
	return nil
}

func (mv *messageValidator) updatePartialSignatureState(
	partialSignatureMessages *spectypes.PartialSignatureMessages,
	receivedFrom peer.ID,
	state *ValidatorState,
	signer spectypes.OperatorID,
	committeeInfo CommitteeInfo,
) error {
	messageSlot := partialSignatureMessages.Slot
	messageEpoch := mv.netCfg.EstimatedEpochAtSlot(messageSlot)

	operatorState := state.OperatorState(committeeInfo.signerIndex(signer))

	signerState := operatorState.GetSignerStateForSlot(messageSlot)
	if signerState == nil {
		signerState = newSignerState(messageSlot, specqbft.FirstRound)
		operatorState.SetSignerStateForSlot(messageSlot, messageEpoch, signerState)
	}

	err := signerState.Peer(receivedFrom).SeenMsgTypes.RecordPartialSignatureMessage(partialSignatureMessages)
	if err != nil {
		return err
	}
	err = signerState.World.SeenMsgTypes.RecordPartialSignatureMessage(partialSignatureMessages)
	if err != nil {
		return err
	}

	// Record the packet's signing roots per signer, not per peer, so a legitimate re-emission (a
	// dependent_root refresh, a changed builder list) is admitted up to its budget and a repeat is IGNOREd
	// whichever peer relays it (SIP #94 §5/§7, validateDistinctRootBudget).
	switch t := partialSignatureMessages.Type; t {
	case spectypes.ProposerPreferencesPartialSig, spectypes.RequestAuthPartialSig:
		roots := seenRootsFor(signerState, t)
		for _, msg := range partialSignatureMessages.Messages {
			roots.record(msg.SigningRoot)
		}
	default:
		// Every other type is capped by the SeenMsgTypes bits recorded above, not by root.
	}

	return nil
}

func (mv *messageValidator) validPartialSigMsgType(msgType spectypes.PartialSigMsgType) bool {
	switch msgType {
	case spectypes.PostConsensusPartialSig,
		spectypes.RandaoPartialSig,
		ssvtypes.SelectionProofPartialSig,
		ssvtypes.ContributionProofs,
		spectypes.ValidatorRegistrationPartialSig,
		spectypes.VoluntaryExitPartialSig,
		spectypes.AggregatorCommitteePartialSig,
		spectypes.PTCAttesterPartialSig,
		spectypes.ProposerPreferencesPartialSig,
		spectypes.RequestAuthPartialSig:
		return true
	default:
		return false
	}
}

func (mv *messageValidator) partialSignatureTypeMatchesRole(msgType spectypes.PartialSigMsgType, role spectypes.RunnerRole) bool {
	switch role {
	case spectypes.RoleCommittee:
		return msgType == spectypes.PostConsensusPartialSig
	case ssvtypes.RoleAggregator:
		return msgType == spectypes.PostConsensusPartialSig || msgType == ssvtypes.SelectionProofPartialSig
	case spectypes.RoleProposer:
		return msgType == spectypes.PostConsensusPartialSig || msgType == spectypes.RandaoPartialSig
	case ssvtypes.RoleSyncCommitteeContribution:
		return msgType == spectypes.PostConsensusPartialSig || msgType == ssvtypes.ContributionProofs
	case spectypes.RoleValidatorRegistration:
		return msgType == spectypes.ValidatorRegistrationPartialSig
	case spectypes.RoleVoluntaryExit:
		return msgType == spectypes.VoluntaryExitPartialSig
	case spectypes.RoleAggregatorCommittee:
		return msgType == spectypes.AggregatorCommitteePartialSig || msgType == spectypes.PostConsensusPartialSig
	case spectypes.RolePTCAttester:
		return msgType == spectypes.PTCAttesterPartialSig
	case spectypes.RoleProposerPreferences:
		// The role carries both the §5 preference round and its request-auth rounds — same duty cadence,
		// distinct signing domains, so distinct partial-sig types.
		return msgType == spectypes.ProposerPreferencesPartialSig || msgType == spectypes.RequestAuthPartialSig
	default:
		return false
	}
}
