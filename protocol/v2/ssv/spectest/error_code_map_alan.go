//go:build alan_spec
// +build alan_spec

package spectest

import (
	"errors"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const (
	// Legacy Alan fixtures are generated against ssv-spec v1.2.2, which had two
	// extra enum members before UnknownDutyRoleDataErrorCode. Keep explicit legacy
	// values so remapping remains stable even if current enums change again.
	legacyAlanUnmarshalSSZErrorCode int = iota + 1
	legacyAlanFutureMessageErrorCode
	legacyAlanZeroCommitMessagesErrorCode
	legacyAlanNoRunningDutyErrorCode
	legacyAlanCommitMessageInvalidErrorCode
	legacyAlanCommitMessageTypeWrongErrorCode
	legacyAlanWrongMessageHeightErrorCode
	legacyAlanSignerIsNotInCommitteeErrorCode
	legacyAlanNonUniqueSignerErrorCode
	legacyAlanValidatorRegistrationNoConsensusDataErrorCode
	legacyAlanValidatorExitNoConsensusDataErrorCode
	legacyAlanUnknownDutyRoleDataErrorCode
	legacyAlanUnknownBlockVersionErrorCode
	legacyAlanIncorrectNumberOfSignaturesErrorCode
	legacyAlanEmptySignatureErrorCode
	legacyAlanNilSSVMessageErrorCode
	legacyAlanNoSignaturesErrorCode
	legacyAlanNoSignersErrorCode
	legacyAlanZeroSignerNotAllowedErrorCode
	legacyAlanInconsistentSignersErrorCode
	legacyAlanNoPartialSigMessagesErrorCode
	legacyAlanNoRunnerForSlotErrorCode
	legacyAlanSkipConsensusMessageAsInstanceIsDecidedErrorCode
	legacyAlanSkipConsensusMessageAsConsensusHasFinishedErrorCode
	legacyAlanDecodeBeaconVoteErrorCode
	legacyAlanNoBeaconDutiesErrorCode
	legacyAlanNoValidatorSharesErrorCode
	legacyAlanAttestationSourceNotLessThanTargetErrorCode
	legacyAlanCheckpointMismatch
	legacyAlanMessageIDCommitteeIDMismatchErrorCode
	legacyAlanMessageTypeInvalidErrorCode
	legacyAlanMessageRoundInvalidErrorCode
	legacyAlanMessageIdentifierInvalidErrorCode
	legacyAlanReconstructSignatureErrorCode
	legacyAlanSlashableAttestationErrorCode
	legacyAlanDecidedWrongInstanceErrorCode
	legacyAlanValidatorRegistrationNoConsensusPhaseErrorCode
	legacyAlanValidatorRegistrationNoPostConsensusPhaseErrorCode
	legacyAlanValidatorExitNoConsensusPhaseErrorCode
	legacyAlanValidatorExitNoPostConsensusPhaseErrorCode
	legacyAlanSSVMessageHasInvalidSignatureErrorCode
	legacyAlanDutyAlreadyPassedErrorCode
	legacyAlanWrongSigningRootErrorCode
	legacyAlanPartialSigInconsistentSignerErrorCode
	legacyAlanNoDecidedValueErrorCode
	legacyAlanNoRunningConsensusInstanceErrorCode
	legacyAlanConsensusInstanceNotDecidedErrorCode
	legacyAlanPartialSigMessageInvalidSlotErrorCode
	legacyAlanPartialSigMessageFutureSlotErrorCode
	legacyAlanUnknownValidatorIndexErrorCode
	legacyAlanWrongRootsCountErrorCode
	legacyAlanDutyEpochTooFarFutureErrorCode
	legacyAlanWrongBeaconRoleTypeErrorCode
	legacyAlanWrongValidatorIndexErrorCode
	legacyAlanWrongValidatorPubkeyErrorCode
	legacyAlanInstanceStoppedProcessingMessagesErrorCode
	legacyAlanWrongMessageRoundErrorCode
	legacyAlanMessageAllowsOneSignerOnlyErrorCode
	legacyAlanNoProposalForCurrentRoundErrorCode
	legacyAlanPastRoundErrorCode
	legacyAlanProposedDataMismatchErrorCode
	legacyAlanRoundChangeNoQuorumErrorCode
	legacyAlanProposalInvalidErrorCode
	legacyAlanRootHashInvalidErrorCode
	legacyAlanMessageSignatureInvalidErrorCode
	legacyAlanQBFTValueInvalidErrorCode
	legacyAlanPrepareMessageInvalidErrorCode
	legacyAlanJustificationsNoQuorumInvalidErrorCode
	legacyAlanProposalLeaderInvalidErrorCode
	legacyAlanInstanceAlreadyRunningErrorCode
	legacyAlanStartInstanceErrorCode
	legacyAlanTimeoutInstanceErrorCode
)

var legacyAlanExpectedErrorCodeMap = map[int]int{
	legacyAlanUnmarshalSSZErrorCode:                               spectypes.UnmarshalSSZErrorCode,
	legacyAlanFutureMessageErrorCode:                              spectypes.FutureMessageErrorCode,
	legacyAlanZeroCommitMessagesErrorCode:                         spectypes.ZeroCommitMessagesErrorCode,
	legacyAlanNoRunningDutyErrorCode:                              spectypes.NoRunningDutyErrorCode,
	legacyAlanCommitMessageInvalidErrorCode:                       spectypes.CommitMessageInvalidErrorCode,
	legacyAlanCommitMessageTypeWrongErrorCode:                     spectypes.CommitMessageTypeWrongErrorCode,
	legacyAlanWrongMessageHeightErrorCode:                         spectypes.WrongMessageHeightErrorCode,
	legacyAlanSignerIsNotInCommitteeErrorCode:                     spectypes.SignerIsNotInCommitteeErrorCode,
	legacyAlanNonUniqueSignerErrorCode:                            spectypes.NonUniqueSignerErrorCode,
	legacyAlanValidatorRegistrationNoConsensusDataErrorCode:       spectypes.ValidatorRegistrationNoConsensusPhaseErrorCode,
	legacyAlanValidatorExitNoConsensusDataErrorCode:               spectypes.ValidatorExitNoConsensusPhaseErrorCode,
	legacyAlanUnknownDutyRoleDataErrorCode:                        spectypes.UnknownDutyRoleDataErrorCode,
	legacyAlanUnknownBlockVersionErrorCode:                        spectypes.UnknownBlockVersionErrorCode,
	legacyAlanIncorrectNumberOfSignaturesErrorCode:                spectypes.IncorrectNumberOfSignaturesErrorCode,
	legacyAlanEmptySignatureErrorCode:                             spectypes.EmptySignatureErrorCode,
	legacyAlanNilSSVMessageErrorCode:                              spectypes.NilSSVMessageErrorCode,
	legacyAlanNoSignaturesErrorCode:                               spectypes.NoSignaturesErrorCode,
	legacyAlanNoSignersErrorCode:                                  spectypes.NoSignersErrorCode,
	legacyAlanZeroSignerNotAllowedErrorCode:                       spectypes.ZeroSignerNotAllowedErrorCode,
	legacyAlanInconsistentSignersErrorCode:                        spectypes.InconsistentSignersErrorCode,
	legacyAlanNoPartialSigMessagesErrorCode:                       spectypes.NoPartialSigMessagesErrorCode,
	legacyAlanNoRunnerForSlotErrorCode:                            spectypes.NoRunnerForSlotErrorCode,
	legacyAlanSkipConsensusMessageAsInstanceIsDecidedErrorCode:    spectypes.SkipConsensusMessageAsInstanceIsDecidedErrorCode,
	legacyAlanSkipConsensusMessageAsConsensusHasFinishedErrorCode: spectypes.SkipConsensusMessageAsConsensusHasFinishedErrorCode,
	legacyAlanDecodeBeaconVoteErrorCode:                           spectypes.DecodeBeaconVoteErrorCode,
	legacyAlanNoBeaconDutiesErrorCode:                             spectypes.NoBeaconDutiesErrorCode,
	legacyAlanNoValidatorSharesErrorCode:                          spectypes.NoValidatorSharesErrorCode,
	legacyAlanAttestationSourceNotLessThanTargetErrorCode:         spectypes.AttestationSourceNotLessThanTargetErrorCode,
	legacyAlanCheckpointMismatch:                                  spectypes.CheckpointMismatch,
	legacyAlanMessageIDCommitteeIDMismatchErrorCode:               spectypes.MessageIDCommitteeIDMismatchErrorCode,
	legacyAlanMessageTypeInvalidErrorCode:                         spectypes.MessageTypeInvalidErrorCode,
	legacyAlanMessageRoundInvalidErrorCode:                        spectypes.MessageRoundInvalidErrorCode,
	legacyAlanMessageIdentifierInvalidErrorCode:                   spectypes.MessageIdentifierInvalidErrorCode,
	legacyAlanReconstructSignatureErrorCode:                       spectypes.ReconstructSignatureErrorCode,
	legacyAlanSlashableAttestationErrorCode:                       spectypes.SlashableAttestationErrorCode,
	legacyAlanDecidedWrongInstanceErrorCode:                       spectypes.DecidedWrongInstanceErrorCode,
	legacyAlanValidatorRegistrationNoConsensusPhaseErrorCode:      spectypes.ValidatorRegistrationNoConsensusPhaseErrorCode,
	legacyAlanValidatorRegistrationNoPostConsensusPhaseErrorCode:  spectypes.ValidatorRegistrationNoPostConsensusPhaseErrorCode,
	legacyAlanValidatorExitNoConsensusPhaseErrorCode:              spectypes.ValidatorExitNoConsensusPhaseErrorCode,
	legacyAlanValidatorExitNoPostConsensusPhaseErrorCode:          spectypes.ValidatorExitNoPostConsensusPhaseErrorCode,
	legacyAlanSSVMessageHasInvalidSignatureErrorCode:              spectypes.SSVMessageHasInvalidSignatureErrorCode,
	legacyAlanDutyAlreadyPassedErrorCode:                          spectypes.DutyAlreadyPassedErrorCode,
	legacyAlanWrongSigningRootErrorCode:                           spectypes.WrongSigningRootErrorCode,
	legacyAlanPartialSigInconsistentSignerErrorCode:               spectypes.PartialSigInconsistentSignerErrorCode,
	legacyAlanNoDecidedValueErrorCode:                             spectypes.NoDecidedValueErrorCode,
	legacyAlanNoRunningConsensusInstanceErrorCode:                 spectypes.NoRunningConsensusInstanceErrorCode,
	legacyAlanConsensusInstanceNotDecidedErrorCode:                spectypes.ConsensusInstanceNotDecidedErrorCode,
	legacyAlanPartialSigMessageInvalidSlotErrorCode:               spectypes.PartialSigMessageInvalidSlotErrorCode,
	legacyAlanPartialSigMessageFutureSlotErrorCode:                spectypes.PartialSigMessageFutureSlotErrorCode,
	legacyAlanUnknownValidatorIndexErrorCode:                      spectypes.UnknownValidatorIndexErrorCode,
	legacyAlanWrongRootsCountErrorCode:                            spectypes.WrongRootsCountErrorCode,
	legacyAlanDutyEpochTooFarFutureErrorCode:                      spectypes.DutyEpochTooFarFutureErrorCode,
	legacyAlanWrongBeaconRoleTypeErrorCode:                        spectypes.WrongBeaconRoleTypeErrorCode,
	legacyAlanWrongValidatorIndexErrorCode:                        spectypes.WrongValidatorIndexErrorCode,
	legacyAlanWrongValidatorPubkeyErrorCode:                       spectypes.WrongValidatorPubkeyErrorCode,
	legacyAlanInstanceStoppedProcessingMessagesErrorCode:          spectypes.InstanceStoppedProcessingMessagesErrorCode,
	legacyAlanWrongMessageRoundErrorCode:                          spectypes.WrongMessageRoundErrorCode,
	legacyAlanMessageAllowsOneSignerOnlyErrorCode:                 spectypes.MessageAllowsOneSignerOnlyErrorCode,
	legacyAlanNoProposalForCurrentRoundErrorCode:                  spectypes.NoProposalForCurrentRoundErrorCode,
	legacyAlanPastRoundErrorCode:                                  spectypes.PastRoundErrorCode,
	legacyAlanProposedDataMismatchErrorCode:                       spectypes.ProposedDataMismatchErrorCode,
	legacyAlanRoundChangeNoQuorumErrorCode:                        spectypes.RoundChangeNoQuorumErrorCode,
	legacyAlanProposalInvalidErrorCode:                            spectypes.ProposalInvalidErrorCode,
	legacyAlanRootHashInvalidErrorCode:                            spectypes.RootHashInvalidErrorCode,
	legacyAlanMessageSignatureInvalidErrorCode:                    spectypes.MessageSignatureInvalidErrorCode,
	legacyAlanQBFTValueInvalidErrorCode:                           spectypes.QBFTValueInvalidErrorCode,
	legacyAlanPrepareMessageInvalidErrorCode:                      spectypes.PrepareMessageInvalidErrorCode,
	legacyAlanJustificationsNoQuorumInvalidErrorCode:              spectypes.JustificationsNoQuorumInvalidErrorCode,
	legacyAlanProposalLeaderInvalidErrorCode:                      spectypes.ProposalLeaderInvalidErrorCode,
	legacyAlanInstanceAlreadyRunningErrorCode:                     spectypes.InstanceAlreadyRunningErrorCode,
	legacyAlanStartInstanceErrorCode:                              spectypes.StartInstanceErrorCode,
	legacyAlanTimeoutInstanceErrorCode:                            spectypes.TimeoutInstanceErrorCode,
}

var alanActualErrorCodeOverrides = map[int]int{
	spectypes.PostConsensusQuorumWithInvalidSignatures: spectypes.ReconstructSignatureErrorCode,
}

func adjustActualError(err error) error {
	if err == nil {
		return nil
	}

	var specErr *spectypes.Error
	if !errors.As(err, &specErr) {
		return err
	}

	if mapped, ok := alanActualErrorCodeOverrides[specErr.Code]; ok && mapped != specErr.Code {
		return spectypes.WrapError(mapped, err)
	}

	return err
}

func adjustExpectedErrorCode(code int) int {
	if mapped, ok := legacyAlanExpectedErrorCodeMap[code]; ok {
		return mapped
	}

	return code
}
