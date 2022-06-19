package ssv

import (
	"github.com/bloxapp/ssv/spec/types"
	"github.com/pkg/errors"
)

// StartDuty starts a duty for the validator
func (v *Validator) StartDuty(duty *types.Duty) error {
	dutyRunner := v.DutyRunners[duty.Type]
	if dutyRunner == nil {
		return errors.Errorf("duty type %s not supported", duty.Type.String())
	}

	if err := dutyRunner.StartNewDuty(duty); err != nil {
		return errors.Wrap(err, "can't start new duty")
	}

	switch dutyRunner.BeaconRoleType {
	case types.BNRoleAttester:
		return v.executeAttestationDuty(duty, dutyRunner)
	case types.BNRoleProposer:
		return v.executeBlockProposalDuty(duty, dutyRunner)
	case types.BNRoleAggregator:
		return v.executeAggregatorDuty(duty, dutyRunner)
	case types.BNRoleSyncCommittee:
		return v.executeSyncCommitteeDuty(duty, dutyRunner)
	case types.BNRoleSyncCommitteeContribution:
		return v.executeSyncCommitteeContributionDuty(duty, dutyRunner)
	default:
		return errors.Errorf("duty type %s unkwon", duty.Type.String())
	}
	return nil
}

// executeSyncCommitteeContributionDuty steps:
// 1) sign a partial contribution proof (for each subcommittee index) and wait for 2f+1 partial sigs from peers
// 2) Reconstruct contribution proofs, check IsSyncCommitteeAggregator and start consensus on duty + contribution data
// 3) Once consensus decides, sign partial contribution data (for each subcommittee) and broadcast
// 4) collect 2f+1 partial sigs, reconstruct and broadcast valid SignedContributionAndProof (for each subcommittee) sig to the BN
func (v *Validator) executeSyncCommitteeContributionDuty(duty *types.Duty, dutyRunner *Runner) error {
	indexes, err := v.beacon.GetSyncSubcommitteeIndex(duty.Slot, duty.PubKey)
	if err != nil {
		return errors.Wrap(err, "failed fetching sync subcommittee indexes")
	}

	msgs, err := dutyRunner.SignSyncSubCommitteeContributionProof(duty.Slot, indexes, v.signer)
	if err != nil {
		return errors.Wrap(err, "could not sign contribution proofs for pre-consensus")
	}

	// package into signed partial sig
	signature, err := v.signer.SignRoot(msgs, types.PartialSignatureType, v.share.SharePubKey)
	if err != nil {
		return errors.Wrap(err, "could not sign PartialSignatureMessage for contribution proofs")
	}
	signedPartialMsg := &SignedPartialSignatureMessage{
		Type:      ContributionProofs,
		Messages:  msgs,
		Signature: signature,
		Signers:   []types.OperatorID{v.share.OperatorID},
	}

	// broadcast
	data, err := signedPartialMsg.Encode()
	if err != nil {
		return errors.Wrap(err, "failed to encode contribution proofs pre-consensus signature msg")
	}
	msgToBroadcast := &types.SSVMessage{
		MsgType: types.SSVPartialSignatureMsgType,
		MsgID:   types.NewMsgID(v.share.ValidatorPubKey, dutyRunner.BeaconRoleType),
		Data:    data,
	}
	if err := v.network.Broadcast(msgToBroadcast); err != nil {
		return errors.Wrap(err, "can't broadcast partial contribution proof sig")
	}
	return nil
}

// executeSyncCommitteeDuty steps:
// 1) get sync block root from BN
// 2) start consensus on duty + block root data
// 3) Once consensus decides, sign partial block root and broadcast
// 4) collect 2f+1 partial sigs, reconstruct and broadcast valid sync committee sig to the BN
func (v *Validator) executeSyncCommitteeDuty(duty *types.Duty, dutyRunner *Runner) error {
	// TODO - waitOneThirdOrValidBlock

	root, err := v.beacon.GetSyncMessageBlockRoot()
	if err != nil {
		return errors.Wrap(err, "failed to get sync committee block root")
	}

	input := &types.ConsensusData{
		Duty:                   duty,
		SyncCommitteeBlockRoot: root,
	}

	if err := dutyRunner.Decide(input); err != nil {
		return errors.Wrap(err, "can't start new duty runner instance for duty")
	}
	return nil
}

// executeAggregatorDuty steps:
// 1) sign a partial selection proof and wait for 2f+1 partial sigs from peers
// 2) reconstruct selection proof and send SubmitAggregateSelectionProof to BN
// 3) start consensus on duty + aggregation data
// 4) Once consensus decides, sign partial aggregation data and broadcast
// 5) collect 2f+1 partial sigs, reconstruct and broadcast valid SignedAggregateSubmitRequest sig to the BN
func (v *Validator) executeAggregatorDuty(duty *types.Duty, dutyRunner *Runner) error {
	msg, err := dutyRunner.SignSlotWithSelectionProofPreConsensus(duty.Slot, v.signer)
	if err != nil {
		return errors.Wrap(err, "could not sign slot with selection proof for pre-consensus")
	}

	signature, err := v.signer.SignRoot(msg, types.PartialSignatureType, v.share.SharePubKey)
	if err != nil {
		return errors.Wrap(err, "could not sign PartialSignatureMessage for selection proof")
	}
	signedPartialMsg := &SignedPartialSignatureMessage{
		Type:      SelectionProofPartialSig,
		Messages:  PartialSignatureMessages{msg},
		Signature: signature,
		Signers:   []types.OperatorID{v.share.OperatorID},
	}

	// broadcast
	data, err := signedPartialMsg.Encode()
	if err != nil {
		return errors.Wrap(err, "failed to encode selection proof pre-consensus signature msg")
	}
	msgToBroadcast := &types.SSVMessage{
		MsgType: types.SSVPartialSignatureMsgType,
		MsgID:   types.NewMsgID(v.share.ValidatorPubKey, dutyRunner.BeaconRoleType),
		Data:    data,
	}
	if err := v.network.Broadcast(msgToBroadcast); err != nil {
		return errors.Wrap(err, "can't broadcast partial selection proof sig")
	}
	return nil
}

// executeBlockProposalDuty steps:
// 1) sign a partial randao sig and wait for 2f+1 partial sigs from peers
// 2) reconstruct randao and send GetBeaconBlock to BN
// 3) start consensus on duty + block data
// 4) Once consensus decides, sign partial block and broadcast
// 5) collect 2f+1 partial sigs, reconstruct and broadcast valid block sig to the BN
func (v *Validator) executeBlockProposalDuty(duty *types.Duty, dutyRunner *Runner) error {
	// sign partial randao
	epoch := v.beacon.GetBeaconNetwork().EstimatedEpochAtSlot(duty.Slot)

	msg, err := dutyRunner.SignRandaoPreConsensus(epoch, duty.Slot, v.signer)
	if err != nil {
		return errors.Wrap(err, "could not sign randao for pre-consensus")
	}

	signature, err := v.signer.SignRoot(msg, types.PartialSignatureType, v.share.SharePubKey)
	if err != nil {
		return errors.Wrap(err, "could not sign PartialSignatureMessage for RandaoPartialSig")
	}
	signedPartialMsg := &SignedPartialSignatureMessage{
		Type:      RandaoPartialSig,
		Messages:  PartialSignatureMessages{msg},
		Signature: signature,
		Signers:   []types.OperatorID{v.share.OperatorID},
	}

	// broadcast
	data, err := signedPartialMsg.Encode()
	if err != nil {
		return errors.Wrap(err, "failed to encode randao pre-consensus signature msg")
	}
	msgToBroadcast := &types.SSVMessage{
		MsgType: types.SSVPartialSignatureMsgType,
		MsgID:   types.NewMsgID(v.share.ValidatorPubKey, dutyRunner.BeaconRoleType),
		Data:    data,
	}
	if err := v.network.Broadcast(msgToBroadcast); err != nil {
		return errors.Wrap(err, "can't broadcast partial randao sig")
	}
	return nil
}

// executeAttestationDuty steps:
// 1) get attestation data from BN
// 2) start consensus on duty + attestation data
// 3) Once consensus decides, sign partial attestation and broadcast
// 4) collect 2f+1 partial sigs, reconstruct and broadcast valid attestation sig to the BN
func (v *Validator) executeAttestationDuty(duty *types.Duty, dutyRunner *Runner) error {
	// TODO - waitOneThirdOrValidBlock

	attData, err := v.beacon.GetAttestationData(duty.Slot, duty.CommitteeIndex)
	if err != nil {
		return errors.Wrap(err, "failed to get attestation data")
	}

	input := &types.ConsensusData{
		Duty:            duty,
		AttestationData: attData,
	}

	if err := dutyRunner.Decide(input); err != nil {
		return errors.Wrap(err, "can't start new duty runner instance for duty")
	}
	return nil
}
