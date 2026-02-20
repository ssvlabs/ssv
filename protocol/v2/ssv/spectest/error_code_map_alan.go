//go:build alan_spec

package spectest

import (
	"errors"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const (
	// Legacy Alan fixtures are generated against ssv-spec v1.2.2, which had two
	// extra enum members before UnknownDutyRoleDataErrorCode. Keep explicit legacy
	// values so remapping remains stable even if current enums change again.
	AlanUnmarshalSSZErrorCode int = iota + 1
	AlanFutureMessageErrorCode
	AlanZeroCommitMessagesErrorCode
	AlanNoRunningDutyErrorCode
	AlanCommitMessageInvalidErrorCode
	AlanCommitMessageTypeWrongErrorCode
	AlanWrongMessageHeightErrorCode
	AlanSignerIsNotInCommitteeErrorCode
	AlanNonUniqueSignerErrorCode
	AlanValidatorRegistrationNoConsensusDataErrorCode
	AlanValidatorExitNoConsensusDataErrorCode
	AlanUnknownDutyRoleDataErrorCode
	AlanUnknownBlockVersionErrorCode
	AlanIncorrectNumberOfSignaturesErrorCode
	AlanEmptySignatureErrorCode
	AlanNilSSVMessageErrorCode
	AlanNoSignaturesErrorCode
	AlanNoSignersErrorCode
	AlanZeroSignerNotAllowedErrorCode
	AlanInconsistentSignersErrorCode
	AlanNoPartialSigMessagesErrorCode
	AlanNoRunnerForSlotErrorCode
	AlanSkipConsensusMessageAsInstanceIsDecidedErrorCode
	AlanSkipConsensusMessageAsConsensusHasFinishedErrorCode
	AlanDecodeBeaconVoteErrorCode
	AlanNoBeaconDutiesErrorCode
	AlanNoValidatorSharesErrorCode
	AlanAttestationSourceNotLessThanTargetErrorCode
	AlanCheckpointMismatch
	AlanMessageIDCommitteeIDMismatchErrorCode
	AlanMessageTypeInvalidErrorCode
	AlanMessageRoundInvalidErrorCode
	AlanMessageIdentifierInvalidErrorCode
	AlanReconstructSignatureErrorCode
	AlanSlashableAttestationErrorCode
	AlanDecidedWrongInstanceErrorCode
	AlanValidatorRegistrationNoConsensusPhaseErrorCode
	AlanValidatorRegistrationNoPostConsensusPhaseErrorCode
	AlanValidatorExitNoConsensusPhaseErrorCode
	AlanValidatorExitNoPostConsensusPhaseErrorCode
	AlanSSVMessageHasInvalidSignatureErrorCode
	AlanDutyAlreadyPassedErrorCode
	AlanWrongSigningRootErrorCode
	AlanPartialSigInconsistentSignerErrorCode
	AlanNoDecidedValueErrorCode
	AlanNoRunningConsensusInstanceErrorCode
	AlanConsensusInstanceNotDecidedErrorCode
	AlanPartialSigMessageInvalidSlotErrorCode
	AlanPartialSigMessageFutureSlotErrorCode
	AlanUnknownValidatorIndexErrorCode
	AlanWrongRootsCountErrorCode
	AlanDutyEpochTooFarFutureErrorCode
	AlanWrongBeaconRoleTypeErrorCode
	AlanWrongValidatorIndexErrorCode
	AlanWrongValidatorPubkeyErrorCode
	AlanInstanceStoppedProcessingMessagesErrorCode
	AlanWrongMessageRoundErrorCode
	AlanMessageAllowsOneSignerOnlyErrorCode
	AlanNoProposalForCurrentRoundErrorCode
	AlanPastRoundErrorCode
	AlanProposedDataMismatchErrorCode
	AlanRoundChangeNoQuorumErrorCode
	AlanProposalInvalidErrorCode
	AlanRootHashInvalidErrorCode
	AlanMessageSignatureInvalidErrorCode
	AlanQBFTValueInvalidErrorCode
	AlanPrepareMessageInvalidErrorCode
	AlanJustificationsNoQuorumInvalidErrorCode
	AlanProposalLeaderInvalidErrorCode
	AlanInstanceAlreadyRunningErrorCode
	AlanStartInstanceErrorCode
	AlanTimeoutInstanceErrorCode
)

var AlanExpectedErrorCodeMap = map[int]int{
	AlanUnmarshalSSZErrorCode:                               spectypes.UnmarshalSSZErrorCode,
	AlanFutureMessageErrorCode:                              spectypes.FutureMessageErrorCode,
	AlanZeroCommitMessagesErrorCode:                         spectypes.ZeroCommitMessagesErrorCode,
	AlanNoRunningDutyErrorCode:                              spectypes.NoRunningDutyErrorCode,
	AlanCommitMessageInvalidErrorCode:                       spectypes.CommitMessageInvalidErrorCode,
	AlanCommitMessageTypeWrongErrorCode:                     spectypes.CommitMessageTypeWrongErrorCode,
	AlanWrongMessageHeightErrorCode:                         spectypes.WrongMessageHeightErrorCode,
	AlanSignerIsNotInCommitteeErrorCode:                     spectypes.SignerIsNotInCommitteeErrorCode,
	AlanNonUniqueSignerErrorCode:                            spectypes.NonUniqueSignerErrorCode,
	AlanValidatorRegistrationNoConsensusDataErrorCode:       spectypes.ValidatorRegistrationNoConsensusPhaseErrorCode,
	AlanValidatorExitNoConsensusDataErrorCode:               spectypes.ValidatorExitNoConsensusPhaseErrorCode,
	AlanUnknownDutyRoleDataErrorCode:                        spectypes.UnknownDutyRoleDataErrorCode,
	AlanUnknownBlockVersionErrorCode:                        spectypes.UnknownBlockVersionErrorCode,
	AlanIncorrectNumberOfSignaturesErrorCode:                spectypes.IncorrectNumberOfSignaturesErrorCode,
	AlanEmptySignatureErrorCode:                             spectypes.EmptySignatureErrorCode,
	AlanNilSSVMessageErrorCode:                              spectypes.NilSSVMessageErrorCode,
	AlanNoSignaturesErrorCode:                               spectypes.NoSignaturesErrorCode,
	AlanNoSignersErrorCode:                                  spectypes.NoSignersErrorCode,
	AlanZeroSignerNotAllowedErrorCode:                       spectypes.ZeroSignerNotAllowedErrorCode,
	AlanInconsistentSignersErrorCode:                        spectypes.InconsistentSignersErrorCode,
	AlanNoPartialSigMessagesErrorCode:                       spectypes.NoPartialSigMessagesErrorCode,
	AlanNoRunnerForSlotErrorCode:                            spectypes.NoRunnerForSlotErrorCode,
	AlanSkipConsensusMessageAsInstanceIsDecidedErrorCode:    spectypes.SkipConsensusMessageAsInstanceIsDecidedErrorCode,
	AlanSkipConsensusMessageAsConsensusHasFinishedErrorCode: spectypes.SkipConsensusMessageAsConsensusHasFinishedErrorCode,
	AlanDecodeBeaconVoteErrorCode:                           spectypes.DecodeBeaconVoteErrorCode,
	AlanNoBeaconDutiesErrorCode:                             spectypes.NoBeaconDutiesErrorCode,
	AlanNoValidatorSharesErrorCode:                          spectypes.NoValidatorSharesErrorCode,
	AlanAttestationSourceNotLessThanTargetErrorCode:         spectypes.AttestationSourceNotLessThanTargetErrorCode,
	AlanCheckpointMismatch:                                  spectypes.CheckpointMismatch,
	AlanMessageIDCommitteeIDMismatchErrorCode:               spectypes.MessageIDCommitteeIDMismatchErrorCode,
	AlanMessageTypeInvalidErrorCode:                         spectypes.MessageTypeInvalidErrorCode,
	AlanMessageRoundInvalidErrorCode:                        spectypes.MessageRoundInvalidErrorCode,
	AlanMessageIdentifierInvalidErrorCode:                   spectypes.MessageIdentifierInvalidErrorCode,
	AlanReconstructSignatureErrorCode:                       spectypes.ReconstructSignatureErrorCode,
	AlanSlashableAttestationErrorCode:                       spectypes.SlashableAttestationErrorCode,
	AlanDecidedWrongInstanceErrorCode:                       spectypes.DecidedWrongInstanceErrorCode,
	AlanValidatorRegistrationNoConsensusPhaseErrorCode:      spectypes.ValidatorRegistrationNoConsensusPhaseErrorCode,
	AlanValidatorRegistrationNoPostConsensusPhaseErrorCode:  spectypes.ValidatorRegistrationNoPostConsensusPhaseErrorCode,
	AlanValidatorExitNoConsensusPhaseErrorCode:              spectypes.ValidatorExitNoConsensusPhaseErrorCode,
	AlanValidatorExitNoPostConsensusPhaseErrorCode:          spectypes.ValidatorExitNoPostConsensusPhaseErrorCode,
	AlanSSVMessageHasInvalidSignatureErrorCode:              spectypes.SSVMessageHasInvalidSignatureErrorCode,
	AlanDutyAlreadyPassedErrorCode:                          spectypes.DutyAlreadyPassedErrorCode,
	AlanWrongSigningRootErrorCode:                           spectypes.WrongSigningRootErrorCode,
	AlanPartialSigInconsistentSignerErrorCode:               spectypes.PartialSigInconsistentSignerErrorCode,
	AlanNoDecidedValueErrorCode:                             spectypes.NoDecidedValueErrorCode,
	AlanNoRunningConsensusInstanceErrorCode:                 spectypes.NoRunningConsensusInstanceErrorCode,
	AlanConsensusInstanceNotDecidedErrorCode:                spectypes.ConsensusInstanceNotDecidedErrorCode,
	AlanPartialSigMessageInvalidSlotErrorCode:               spectypes.PartialSigMessageInvalidSlotErrorCode,
	AlanPartialSigMessageFutureSlotErrorCode:                spectypes.PartialSigMessageFutureSlotErrorCode,
	AlanUnknownValidatorIndexErrorCode:                      spectypes.UnknownValidatorIndexErrorCode,
	AlanWrongRootsCountErrorCode:                            spectypes.WrongRootsCountErrorCode,
	AlanDutyEpochTooFarFutureErrorCode:                      spectypes.DutyEpochTooFarFutureErrorCode,
	AlanWrongBeaconRoleTypeErrorCode:                        spectypes.WrongBeaconRoleTypeErrorCode,
	AlanWrongValidatorIndexErrorCode:                        spectypes.WrongValidatorIndexErrorCode,
	AlanWrongValidatorPubkeyErrorCode:                       spectypes.WrongValidatorPubkeyErrorCode,
	AlanInstanceStoppedProcessingMessagesErrorCode:          spectypes.InstanceStoppedProcessingMessagesErrorCode,
	AlanWrongMessageRoundErrorCode:                          spectypes.WrongMessageRoundErrorCode,
	AlanMessageAllowsOneSignerOnlyErrorCode:                 spectypes.MessageAllowsOneSignerOnlyErrorCode,
	AlanNoProposalForCurrentRoundErrorCode:                  spectypes.NoProposalForCurrentRoundErrorCode,
	AlanPastRoundErrorCode:                                  spectypes.PastRoundErrorCode,
	AlanProposedDataMismatchErrorCode:                       spectypes.ProposedDataMismatchErrorCode,
	AlanRoundChangeNoQuorumErrorCode:                        spectypes.RoundChangeNoQuorumErrorCode,
	AlanProposalInvalidErrorCode:                            spectypes.ProposalInvalidErrorCode,
	AlanRootHashInvalidErrorCode:                            spectypes.RootHashInvalidErrorCode,
	AlanMessageSignatureInvalidErrorCode:                    spectypes.MessageSignatureInvalidErrorCode,
	AlanQBFTValueInvalidErrorCode:                           spectypes.QBFTValueInvalidErrorCode,
	AlanPrepareMessageInvalidErrorCode:                      spectypes.PrepareMessageInvalidErrorCode,
	AlanJustificationsNoQuorumInvalidErrorCode:              spectypes.JustificationsNoQuorumInvalidErrorCode,
	AlanProposalLeaderInvalidErrorCode:                      spectypes.ProposalLeaderInvalidErrorCode,
	AlanInstanceAlreadyRunningErrorCode:                     spectypes.InstanceAlreadyRunningErrorCode,
	AlanStartInstanceErrorCode:                              spectypes.StartInstanceErrorCode,
	AlanTimeoutInstanceErrorCode:                            spectypes.TimeoutInstanceErrorCode,
}

func adjustActualError(err error) error {
	if err == nil {
		return nil
	}

	var specErr *spectypes.Error
	if !errors.As(err, &specErr) {
		return err
	}

	if specErr.Code == spectypes.PostConsensusQuorumWithInvalidSignatures {
		return spectypes.WrapError(spectypes.ReconstructSignatureErrorCode, err)
	}

	return err
}

func adjustExpectedErrorCode(code int) int {
	if mapped, ok := AlanExpectedErrorCodeMap[code]; ok {
		return mapped
	}

	return code
}
