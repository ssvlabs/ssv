package testingutils

import (
	"github.com/attestantio/go-eth2-client/spec/altair"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/spec/qbft"
	"github.com/bloxapp/ssv/spec/ssv"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
)

var AttesterMsgID = func() []byte {
	ret := types.NewMsgID(TestingValidatorPubKey[:], types.BNRoleAttester)
	return ret[:]
}()

var ProposerMsgID = func() []byte {
	ret := types.NewMsgID(TestingValidatorPubKey[:], types.BNRoleProposer)
	return ret[:]
}()
var AggregatorMsgID = func() []byte {
	ret := types.NewMsgID(TestingValidatorPubKey[:], types.BNRoleAggregator)
	return ret[:]
}()
var SyncCommitteeMsgID = func() []byte {
	ret := types.NewMsgID(TestingValidatorPubKey[:], types.BNRoleSyncCommittee)
	return ret[:]
}()
var SyncCommitteeContributionMsgID = func() []byte {
	ret := types.NewMsgID(TestingValidatorPubKey[:], types.BNRoleSyncCommitteeContribution)
	return ret[:]
}()

var TestAttesterConsensusData = &types.ConsensusData{
	Duty:            TestingAttesterDuty,
	AttestationData: TestingAttestationData,
}
var TestAttesterConsensusDataByts, _ = TestAttesterConsensusData.Encode()

var TestAggregatorConsensusData = &types.ConsensusData{
	Duty:              TestingAggregatorDuty,
	AggregateAndProof: TestingAggregateAndProof,
}
var TestAggregatorConsensusDataByts, _ = TestAggregatorConsensusData.Encode()

var TestProposerConsensusData = &types.ConsensusData{
	Duty:      TestingProposerDuty,
	BlockData: TestingBeaconBlock,
}
var TestProposerConsensusDataByts, _ = TestProposerConsensusData.Encode()

var TestSyncCommitteeConsensusData = &types.ConsensusData{
	Duty:                   TestingSyncCommitteeDuty,
	SyncCommitteeBlockRoot: TestingSyncCommitteeBlockRoot,
}
var TestSyncCommitteeConsensusDataByts, _ = TestSyncCommitteeConsensusData.Encode()

var TestSyncCommitteeContributionConsensusData = &types.ConsensusData{
	Duty: TestingSyncCommitteeContributionDuty,
	SyncCommitteeContribution: map[spec.BLSSignature]*altair.SyncCommitteeContribution{
		TestingContributionProofsSigned[0]: TestingSyncCommitteeContributions[0],
		TestingContributionProofsSigned[1]: TestingSyncCommitteeContributions[1],
		TestingContributionProofsSigned[2]: TestingSyncCommitteeContributions[2],
	},
}
var TestSyncCommitteeContributionConsensusDataByts, _ = TestSyncCommitteeContributionConsensusData.Encode()

var TestConsensusUnkownDutyTypeData = &types.ConsensusData{
	Duty:            TestingUnknownDutyType,
	AttestationData: TestingAttestationData,
}
var TestConsensusUnkownDutyTypeDataByts, _ = TestConsensusUnkownDutyTypeData.Encode()

var TestConsensusWrongDutyPKData = &types.ConsensusData{
	Duty:            TestingWrongDutyPK,
	AttestationData: TestingAttestationData,
}
var TestConsensusWrongDutyPKDataByts, _ = TestConsensusWrongDutyPKData.Encode()

var SSVMsgAttester = func(qbftMsg *qbft.SignedMessage, partialSigMsg *ssv.SignedPartialSignatureMessage) *types.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, types.NewMsgID(TestingValidatorPubKey[:], types.BNRoleAttester))
}

var SSVMsgWrongID = func(qbftMsg *qbft.SignedMessage, partialSigMsg *ssv.SignedPartialSignatureMessage) *types.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, types.NewMsgID(TestingWrongValidatorPubKey[:], types.BNRoleAttester))
}

var SSVMsgProposer = func(qbftMsg *qbft.SignedMessage, partialSigMsg *ssv.SignedPartialSignatureMessage) *types.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, types.NewMsgID(TestingValidatorPubKey[:], types.BNRoleProposer))
}

var SSVMsgAggregator = func(qbftMsg *qbft.SignedMessage, partialSigMsg *ssv.SignedPartialSignatureMessage) *types.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, types.NewMsgID(TestingValidatorPubKey[:], types.BNRoleAggregator))
}

var SSVMsgSyncCommittee = func(qbftMsg *qbft.SignedMessage, partialSigMsg *ssv.SignedPartialSignatureMessage) *types.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, types.NewMsgID(TestingValidatorPubKey[:], types.BNRoleSyncCommittee))
}

var SSVMsgSyncCommitteeContribution = func(qbftMsg *qbft.SignedMessage, partialSigMsg *ssv.SignedPartialSignatureMessage) *types.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, types.NewMsgID(TestingValidatorPubKey[:], types.BNRoleSyncCommitteeContribution))
}

var ssvMsg = func(qbftMsg *qbft.SignedMessage, postMsg *ssv.SignedPartialSignatureMessage, msgID types.MessageID) *types.SSVMessage {
	var msgType types.MsgType
	var data []byte
	if qbftMsg != nil {
		msgType = types.SSVConsensusMsgType
		data, _ = qbftMsg.Encode()
	} else if postMsg != nil {
		msgType = types.SSVPartialSignatureMsgType
		data, _ = postMsg.Encode()
	} else {
		panic("msg type undefined")
	}

	return &types.SSVMessage{
		MsgType: msgType,
		MsgID:   msgID,
		Data:    data,
	}
}

var PostConsensusAttestationMsgWithMsgMultiSigners = func(sk *bls.SecretKey, id types.OperatorID, height qbft.Height) *ssv.SignedPartialSignatureMessage {
	return postConsensusAttestationMsg(sk, id, height, false, false, true, false)
}

var PostConsensusAttestationMsgWithNoMsgSigners = func(sk *bls.SecretKey, id types.OperatorID, height qbft.Height) *ssv.SignedPartialSignatureMessage {
	return postConsensusAttestationMsg(sk, id, height, false, false, true, false)
}

var PostConsensusAttestationMsgWithWrongSig = func(sk *bls.SecretKey, id types.OperatorID, height qbft.Height) *ssv.SignedPartialSignatureMessage {
	return postConsensusAttestationMsg(sk, id, height, false, true, false, false)
}

var PostConsensusAttestationMsgWithWrongRoot = func(sk *bls.SecretKey, id types.OperatorID, height qbft.Height) *ssv.SignedPartialSignatureMessage {
	return postConsensusAttestationMsg(sk, id, height, true, false, false, false)
}

var PostConsensusAttestationMsg = func(sk *bls.SecretKey, id types.OperatorID, height qbft.Height) *ssv.SignedPartialSignatureMessage {
	return postConsensusAttestationMsg(sk, id, height, false, false, false, false)
}

var postConsensusAttestationMsg = func(
	sk *bls.SecretKey,
	id types.OperatorID,
	height qbft.Height,
	wrongRoot bool,
	wrongBeaconSig bool,
	noMsgSigners bool,
	multiMsgSigners bool,
) *ssv.SignedPartialSignatureMessage {
	signer := NewTestingKeyManager()
	signedAtt, root, _ := signer.SignAttestation(TestingAttestationData, TestingAttesterDuty, sk.GetPublicKey().Serialize())

	if wrongBeaconSig {
		signedAtt, _, _ = signer.SignAttestation(TestingAttestationData, TestingAttesterDuty, TestingWrongValidatorPubKey[:])
	}

	if wrongRoot {
		root = []byte{1, 2, 3, 4}
	}

	postConsensusMsg := &ssv.PartialSignatureMessage{
		Slot:             TestingDutySlot,
		PartialSignature: signedAtt.Signature[:],
		SigningRoot:      root,
		Signers:          []types.OperatorID{id},
	}

	if noMsgSigners {
		postConsensusMsg.Signers = []types.OperatorID{}
	}
	if multiMsgSigners {
		postConsensusMsg.Signers = []types.OperatorID{id, 5}
	}

	msgs := ssv.PartialSignatureMessages{postConsensusMsg}
	sig, _ := signer.SignRoot(msgs, types.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &ssv.SignedPartialSignatureMessage{
		Type:      ssv.PostConsensusPartialSig,
		Messages:  msgs,
		Signature: sig,
		Signers:   []types.OperatorID{id},
	}
}

var PostConsensusProposerMsg = func(sk *bls.SecretKey, id types.OperatorID) *ssv.SignedPartialSignatureMessage {
	return postConsensusBeaconBlockMsg(sk, id, false, false, false, false)
}

var postConsensusBeaconBlockMsg = func(
	sk *bls.SecretKey,
	id types.OperatorID,
	wrongRoot bool,
	wrongBeaconSig bool,
	noMsgSigners bool,
	multiMsgSigners bool,
) *ssv.SignedPartialSignatureMessage {
	signer := NewTestingKeyManager()
	signedAtt, root, _ := signer.SignBeaconBlock(TestingBeaconBlock, TestingProposerDuty, sk.GetPublicKey().Serialize())

	if wrongBeaconSig {
		//signedAtt, _, _ = signer.SignAttestation(TestingAttestationData, TestingAttesterDuty, TestingWrongSK.GetPublicKey().Serialize())
		panic("implement")
	}

	if wrongRoot {
		root = []byte{1, 2, 3, 4}
	}

	postConsensusMsg := &ssv.PartialSignatureMessage{
		Slot:             TestingDutySlot,
		PartialSignature: signedAtt.Signature[:],
		SigningRoot:      root,
		Signers:          []types.OperatorID{id},
	}

	if noMsgSigners {
		postConsensusMsg.Signers = []types.OperatorID{}
	}
	if multiMsgSigners {
		postConsensusMsg.Signers = []types.OperatorID{id, 5}
	}

	msgs := ssv.PartialSignatureMessages{postConsensusMsg}
	sig, _ := signer.SignRoot(msgs, types.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &ssv.SignedPartialSignatureMessage{
		Type:      ssv.PostConsensusPartialSig,
		Messages:  msgs,
		Signature: sig,
		Signers:   []types.OperatorID{id},
	}
}

var PreConsensusRandaoMsg = func(sk *bls.SecretKey, id types.OperatorID) *ssv.SignedPartialSignatureMessage {
	return randaoMsg(sk, id, false, false, false, false)
}

var randaoMsg = func(
	sk *bls.SecretKey,
	id types.OperatorID,
	wrongRoot bool,
	wrongBeaconSig bool,
	noMsgSigners bool,
	multiMsgSigners bool,
) *ssv.SignedPartialSignatureMessage {
	signer := NewTestingKeyManager()
	randaoSig, root, _ := signer.SignRandaoReveal(TestingDutySlot, sk.GetPublicKey().Serialize())

	randaoMsg := &ssv.PartialSignatureMessage{
		Slot:             TestingDutySlot,
		PartialSignature: randaoSig[:],
		SigningRoot:      root,
		Signers:          []types.OperatorID{id},
	}

	msgs := ssv.PartialSignatureMessages{randaoMsg}
	sig, _ := signer.SignRoot(msgs, types.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &ssv.SignedPartialSignatureMessage{
		Type:      ssv.RandaoPartialSig,
		Messages:  msgs,
		Signature: sig,
		Signers:   []types.OperatorID{id},
	}
}

var PreConsensusSelectionProofMsg = func(sk *bls.SecretKey, id types.OperatorID) *ssv.SignedPartialSignatureMessage {
	return selectionProofMsg(sk, id, false, false, false, false)
}

var selectionProofMsg = func(
	sk *bls.SecretKey,
	id types.OperatorID,
	wrongRoot bool,
	wrongBeaconSig bool,
	noMsgSigners bool,
	multiMsgSigners bool,
) *ssv.SignedPartialSignatureMessage {
	signer := NewTestingKeyManager()
	sig, root, _ := signer.SignSlotWithSelectionProof(TestingDutySlot, sk.GetPublicKey().Serialize())

	msg := &ssv.PartialSignatureMessage{
		Slot:             TestingDutySlot,
		PartialSignature: sig[:],
		SigningRoot:      root,
		Signers:          []types.OperatorID{id},
	}

	msgs := ssv.PartialSignatureMessages{msg}
	msgSig, _ := signer.SignRoot(msgs, types.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &ssv.SignedPartialSignatureMessage{
		Type:      ssv.SelectionProofPartialSig,
		Messages:  msgs,
		Signature: msgSig,
		Signers:   []types.OperatorID{id},
	}
}

var PostConsensusAggregatorMsg = func(sk *bls.SecretKey, id types.OperatorID) *ssv.SignedPartialSignatureMessage {
	return postConsensusAggregatorMsg(sk, id, false, false, false, false)
}

var postConsensusAggregatorMsg = func(
	sk *bls.SecretKey,
	id types.OperatorID,
	wrongRoot bool,
	wrongBeaconSig bool,
	noMsgSigners bool,
	multiMsgSigners bool,
) *ssv.SignedPartialSignatureMessage {
	signer := NewTestingKeyManager()
	signedAtt, root, _ := signer.SignAggregateAndProof(TestingAggregateAndProof, TestingProposerDuty, sk.GetPublicKey().Serialize())

	if wrongBeaconSig {
		//signedAtt, _, _ = signer.SignAttestation(TestingAttestationData, TestingAttesterDuty, TestingWrongSK.GetPublicKey().Serialize())
		panic("implement")
	}

	if wrongRoot {
		root = []byte{1, 2, 3, 4}
	}

	postConsensusMsg := &ssv.PartialSignatureMessage{
		Slot:             TestingDutySlot,
		PartialSignature: signedAtt.Signature[:],
		SigningRoot:      root,
		Signers:          []types.OperatorID{id},
	}

	if noMsgSigners {
		postConsensusMsg.Signers = []types.OperatorID{}
	}
	if multiMsgSigners {
		postConsensusMsg.Signers = []types.OperatorID{id, 5}
	}

	msgs := ssv.PartialSignatureMessages{postConsensusMsg}
	sig, _ := signer.SignRoot(msgs, types.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &ssv.SignedPartialSignatureMessage{
		Type:      ssv.PostConsensusPartialSig,
		Messages:  msgs,
		Signature: sig,
		Signers:   []types.OperatorID{id},
	}
}

var PostConsensusSyncCommitteeMsg = func(sk *bls.SecretKey, id types.OperatorID) *ssv.SignedPartialSignatureMessage {
	return postConsensusSyncCommitteeMsg(sk, id, false, false, false, false)
}

var postConsensusSyncCommitteeMsg = func(
	sk *bls.SecretKey,
	id types.OperatorID,
	wrongRoot bool,
	wrongBeaconSig bool,
	noMsgSigners bool,
	multiMsgSigners bool,
) *ssv.SignedPartialSignatureMessage {
	signer := NewTestingKeyManager()
	signedRoot, root, _ := signer.SignSyncCommitteeBlockRoot(TestingDutySlot, TestingSyncCommitteeBlockRoot, TestingSyncCommitteeDuty.ValidatorIndex, sk.GetPublicKey().Serialize())

	if wrongBeaconSig {
		//signedAtt, _, _ = signer.SignAttestation(TestingAttestationData, TestingAttesterDuty, TestingWrongSK.GetPublicKey().Serialize())
		panic("implement")
	}

	if wrongRoot {
		root = []byte{1, 2, 3, 4}
	}

	postConsensusMsg := &ssv.PartialSignatureMessage{
		Slot:             TestingDutySlot,
		PartialSignature: signedRoot.Signature[:],
		SigningRoot:      root,
		Signers:          []types.OperatorID{id},
	}

	if noMsgSigners {
		postConsensusMsg.Signers = []types.OperatorID{}
	}
	if multiMsgSigners {
		postConsensusMsg.Signers = []types.OperatorID{id, 5}
	}

	msgs := ssv.PartialSignatureMessages{postConsensusMsg}
	sig, _ := signer.SignRoot(msgs, types.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &ssv.SignedPartialSignatureMessage{
		Type:      ssv.PostConsensusPartialSig,
		Messages:  msgs,
		Signature: sig,
		Signers:   []types.OperatorID{id},
	}
}

var PreConsensusContributionProofMsg = func(sk *bls.SecretKey, id types.OperatorID) *ssv.SignedPartialSignatureMessage {
	return contributionProofMsg(sk, id, false, false, false, false)
}

var contributionProofMsg = func(
	sk *bls.SecretKey,
	id types.OperatorID,
	wrongRoot bool,
	wrongBeaconSig bool,
	noMsgSigners bool,
	multiMsgSigners bool,
) *ssv.SignedPartialSignatureMessage {
	signer := NewTestingKeyManager()
	msgs := ssv.PartialSignatureMessages{}
	for index, _ := range TestingContributionProofRoots {
		sig, root, _ := signer.SignContributionProof(TestingDutySlot, uint64(index), sk.GetPublicKey().Serialize())
		msg := &ssv.PartialSignatureMessage{
			Slot:             TestingDutySlot,
			PartialSignature: sig[:],
			SigningRoot:      root,
			Signers:          []types.OperatorID{id},
			MetaData: &ssv.PartialSignatureMetaData{
				ContributionSubCommitteeIndex: uint64(index),
			},
		}
		msgs = append(msgs, msg)
	}

	msgSig, _ := signer.SignRoot(msgs, types.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &ssv.SignedPartialSignatureMessage{
		Type:      ssv.ContributionProofs,
		Messages:  msgs,
		Signature: msgSig,
		Signers:   []types.OperatorID{id},
	}
}

var PostConsensusSyncCommitteeContributionMsg = func(sk *bls.SecretKey, id types.OperatorID, keySet *TestKeySet) *ssv.SignedPartialSignatureMessage {
	return postConsensusSyncCommitteeContributionMsg(sk, id, TestingValidatorIndex, keySet, false, false, false, false)
}

var postConsensusSyncCommitteeContributionMsg = func(
	sk *bls.SecretKey,
	id types.OperatorID,
	validatorIndex spec.ValidatorIndex,
	keySet *TestKeySet,
	wrongRoot bool,
	wrongBeaconSig bool,
	noMsgSigners bool,
	multiMsgSigners bool,
) *ssv.SignedPartialSignatureMessage {
	signer := NewTestingKeyManager()

	msgs := ssv.PartialSignatureMessages{}
	for index, c := range TestingSyncCommitteeContributions {
		signedProof, _, _ := signer.SignContributionProof(TestingDutySlot, uint64(index), keySet.ValidatorSK.GetPublicKey().Serialize())
		signedProofbls := spec.BLSSignature{}
		copy(signedProofbls[:], signedProof)

		signed, root, _ := signer.SignContribution(&altair.ContributionAndProof{
			AggregatorIndex: validatorIndex,
			Contribution:    c,
			SelectionProof:  signedProofbls,
		}, sk.GetPublicKey().Serialize())

		if wrongRoot {
			root = []byte{1, 2, 3, 4}
		}

		msg := &ssv.PartialSignatureMessage{
			Slot:             TestingDutySlot,
			PartialSignature: signed.Signature[:],
			SigningRoot:      root,
			Signers:          []types.OperatorID{id},
		}

		if noMsgSigners {
			msg.Signers = []types.OperatorID{}
		}
		if multiMsgSigners {
			msg.Signers = []types.OperatorID{id, 5}
		}
		if wrongBeaconSig {
			//signedAtt, _, _ = signer.SignAttestation(TestingAttestationData, TestingAttesterDuty, TestingWrongSK.GetPublicKey().Serialize())
			panic("implement")
		}

		msgs = append(msgs, msg)
	}

	sig, _ := signer.SignRoot(msgs, types.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &ssv.SignedPartialSignatureMessage{
		Type:      ssv.PostConsensusPartialSig,
		Messages:  msgs,
		Signature: sig,
		Signers:   []types.OperatorID{id},
	}
}
