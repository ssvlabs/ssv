package utils

import (
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/ssv"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/herumi/bls-eth-go-binary/bls"
)

var AttesterMsgID = func() []byte {
	ret := types.NewMsgID(testingutils.TestingValidatorPubKey[:], types.BNRoleAttester)
	return ret[:]
}()

var ProposerMsgID = func() []byte {
	ret := types.NewMsgID(testingutils.TestingValidatorPubKey[:], types.BNRoleProposer)
	return ret[:]
}()
var AggregatorMsgID = func() []byte {
	ret := types.NewMsgID(testingutils.TestingValidatorPubKey[:], types.BNRoleAggregator)
	return ret[:]
}()
var SyncCommitteeMsgID = func() []byte {
	ret := types.NewMsgID(testingutils.TestingValidatorPubKey[:], types.BNRoleSyncCommittee)
	return ret[:]
}()
var SyncCommitteeContributionMsgID = func() []byte {
	ret := types.NewMsgID(testingutils.TestingValidatorPubKey[:], types.BNRoleSyncCommitteeContribution)
	return ret[:]
}()

var TestAttesterConsensusData = &types.ConsensusData{
	Duty:            testingutils.TestingAttesterDuty,
	AttestationData: testingutils.TestingAttestationData,
}
var TestAttesterConsensusDataByts, _ = TestAttesterConsensusData.Encode()

var TestAggregatorConsensusData = &types.ConsensusData{
	Duty:              testingutils.TestingAggregatorDuty,
	AggregateAndProof: testingutils.TestingAggregateAndProof,
}
var TestAggregatorConsensusDataByts, _ = TestAggregatorConsensusData.Encode()

var TestProposerConsensusData = &types.ConsensusData{
	Duty:      testingutils.TestingProposerDuty,
	BlockData: testingutils.TestingBeaconBlock,
}
var TestProposerConsensusDataByts, _ = TestProposerConsensusData.Encode()

var TestSyncCommitteeConsensusData = &types.ConsensusData{
	Duty:                   testingutils.TestingSyncCommitteeDuty,
	SyncCommitteeBlockRoot: testingutils.TestingSyncCommitteeBlockRoot,
}
var TestSyncCommitteeConsensusDataByts, _ = TestSyncCommitteeConsensusData.Encode()

var TestSyncCommitteeContributionConsensusData = &types.ConsensusData{
	Duty: testingutils.TestingSyncCommitteeContributionDuty,
	SyncCommitteeContribution: map[spec.BLSSignature]*altair.SyncCommitteeContribution{
		testingutils.TestingContributionProofsSigned[0]: testingutils.TestingSyncCommitteeContributions[0],
		testingutils.TestingContributionProofsSigned[1]: testingutils.TestingSyncCommitteeContributions[1],
		testingutils.TestingContributionProofsSigned[2]: testingutils.TestingSyncCommitteeContributions[2],
	},
}
var TestSyncCommitteeContributionConsensusDataByts, _ = TestSyncCommitteeContributionConsensusData.Encode()

var TestConsensusUnkownDutyTypeData = &types.ConsensusData{
	Duty:            testingutils.TestingUnknownDutyType,
	AttestationData: testingutils.TestingAttestationData,
}
var TestConsensusUnkownDutyTypeDataByts, _ = TestConsensusUnkownDutyTypeData.Encode()

var TestConsensusWrongDutyPKData = &types.ConsensusData{
	Duty:            testingutils.TestingWrongDutyPK,
	AttestationData: testingutils.TestingAttestationData,
}
var TestConsensusWrongDutyPKDataByts, _ = TestConsensusWrongDutyPKData.Encode()

var SSVMsgAttester = func(qbftMsg *qbft.SignedMessage, partialSigMsg *ssv.SignedPartialSignatureMessage) *types.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, types.NewMsgID(testingutils.TestingValidatorPubKey[:], types.BNRoleAttester))
}

var SSVMsgWrongID = func(qbftMsg *qbft.SignedMessage, partialSigMsg *ssv.SignedPartialSignatureMessage) *types.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, types.NewMsgID(testingutils.TestingWrongValidatorPubKey[:], types.BNRoleAttester))
}

var SSVMsgProposer = func(qbftMsg *qbft.SignedMessage, partialSigMsg *ssv.SignedPartialSignatureMessage) *types.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, types.NewMsgID(testingutils.TestingValidatorPubKey[:], types.BNRoleProposer))
}

var SSVMsgAggregator = func(qbftMsg *qbft.SignedMessage, partialSigMsg *ssv.SignedPartialSignatureMessage) *types.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, types.NewMsgID(testingutils.TestingValidatorPubKey[:], types.BNRoleAggregator))
}

var SSVMsgSyncCommittee = func(qbftMsg *qbft.SignedMessage, partialSigMsg *ssv.SignedPartialSignatureMessage) *types.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, types.NewMsgID(testingutils.TestingValidatorPubKey[:], types.BNRoleSyncCommittee))
}

var SSVMsgSyncCommitteeContribution = func(qbftMsg *qbft.SignedMessage, partialSigMsg *ssv.SignedPartialSignatureMessage) *types.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, types.NewMsgID(testingutils.TestingValidatorPubKey[:], types.BNRoleSyncCommitteeContribution))
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

var PostConsensusAttestationMsgWithWrongSig = func(sk *bls.SecretKey, id types.OperatorID, height qbft.Height) *ssv.SignedPartialSignatureMessage {
	return postConsensusAttestationMsg(sk, id, height, true, false)
}

var PostConsensusAttestationMsgWithWrongRoot = func(sk *bls.SecretKey, id types.OperatorID, height qbft.Height) *ssv.SignedPartialSignatureMessage {
	return postConsensusAttestationMsg(sk, id, height, true, false)
}

var PostConsensusAttestationMsg = func(sk *bls.SecretKey, id types.OperatorID, height qbft.Height) *ssv.SignedPartialSignatureMessage {
	return postConsensusAttestationMsg(sk, id, height, false, false)
}

var postConsensusAttestationMsg = func(
	sk *bls.SecretKey,
	id types.OperatorID,
	height qbft.Height,
	wrongRoot bool,
	wrongBeaconSig bool,
) *ssv.SignedPartialSignatureMessage {
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(testingutils.TestingAttestationData.Target.Epoch, types.DomainAttester)
	signed, root, _ := signer.SignBeaconObject(testingutils.TestingAttestationData, d, sk.GetPublicKey().Serialize())

	if wrongBeaconSig {
		signed, _, _ = signer.SignBeaconObject(testingutils.TestingAttestationData, d, testingutils.TestingWrongValidatorPubKey[:])
	}

	if wrongRoot {
		root = []byte{1, 2, 3, 4}
	}

	msgs := ssv.PartialSignatureMessages{
		Type: ssv.PostConsensusPartialSig,
		Messages: []*ssv.PartialSignatureMessage{
			{
				Slot:             testingutils.TestingDutySlot,
				PartialSignature: signed,
				SigningRoot:      root,
				Signer:           id,
			},
		},
	}
	sig, _ := signer.SignRoot(msgs, types.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &ssv.SignedPartialSignatureMessage{
		Message:   msgs,
		Signature: sig,
		Signer:    id,
	}
}

var PostConsensusProposerMsg = func(sk *bls.SecretKey, id types.OperatorID) *ssv.SignedPartialSignatureMessage {
	return postConsensusBeaconBlockMsg(sk, id, false, false)
}

var postConsensusBeaconBlockMsg = func(
	sk *bls.SecretKey,
	id types.OperatorID,
	wrongRoot bool,
	wrongBeaconSig bool,
) *ssv.SignedPartialSignatureMessage {
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()

	d, _ := beacon.DomainData(1, types.DomainProposer) // epoch doesn't matter here, hard coded
	sig, root, _ := signer.SignBeaconObject(testingutils.TestingBeaconBlock, d, sk.GetPublicKey().Serialize())
	blsSig := spec.BLSSignature{}
	copy(blsSig[:], sig)

	signed := bellatrix.SignedBeaconBlock{
		Message:   testingutils.TestingBeaconBlock,
		Signature: blsSig,
	}

	if wrongBeaconSig {
		//signed, _, _ = signer.SignAttestation(testingutils.TestingAttestationData, testingutils.TestingAttesterDuty, testingutils.TestingWrongSK.GetPublicKey().Serialize())
		panic("implement")
	}

	if wrongRoot {
		root = []byte{1, 2, 3, 4}
	}

	msgs := ssv.PartialSignatureMessages{
		Type: ssv.PostConsensusPartialSig,
		Messages: []*ssv.PartialSignatureMessage{
			{
				Slot:             testingutils.TestingDutySlot,
				PartialSignature: signed.Signature[:],
				SigningRoot:      root,
				Signer:           id,
			},
		},
	}
	msgSig, _ := signer.SignRoot(msgs, types.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &ssv.SignedPartialSignatureMessage{
		Message:   msgs,
		Signature: msgSig,
		Signer:    id,
	}
}

var PreConsensusFailedMsg = func(msgSigner *bls.SecretKey, msgSignerID types.OperatorID) *ssv.SignedPartialSignatureMessage {
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(testingutils.TestingDutyEpoch, types.DomainRandao)
	signed, root, _ := signer.SignBeaconObject(types.SSZUint64(testingutils.TestingDutyEpoch), d, msgSigner.GetPublicKey().Serialize())

	msg := ssv.PartialSignatureMessages{
		Type: ssv.RandaoPartialSig,
		Messages: []*ssv.PartialSignatureMessage{
			{
				Slot:             testingutils.TestingDutySlot,
				PartialSignature: signed[:],
				SigningRoot:      root,
				Signer:           msgSignerID,
			},
		},
	}
	sig, _ := signer.SignRoot(msg, types.PartialSignatureType, msgSigner.GetPublicKey().Serialize())
	return &ssv.SignedPartialSignatureMessage{
		Message:   msg,
		Signature: sig,
		Signer:    msgSignerID,
	}
}

var PreConsensusRandaoMsg = func(sk *bls.SecretKey, id types.OperatorID) *ssv.SignedPartialSignatureMessage {
	return randaoMsg(sk, id, false, testingutils.TestingDutyEpoch, testingutils.TestingDutySlot, 1)
}

// PreConsensusRandaoNextEpochMsg testing for a second duty start
var PreConsensusRandaoNextEpochMsg = func(sk *bls.SecretKey, id types.OperatorID) *ssv.SignedPartialSignatureMessage {
	return randaoMsg(sk, id, false, testingutils.TestingDutyEpoch2, testingutils.TestingDutySlot2, 1)
}

var PreConsensusRandaoDifferentEpochMsg = func(sk *bls.SecretKey, id types.OperatorID) *ssv.SignedPartialSignatureMessage {
	return randaoMsg(sk, id, false, testingutils.TestingDutyEpoch+1, testingutils.TestingDutySlot, 1)
}

var PreConsensusRandaoWrongSlotMsg = func(sk *bls.SecretKey, id types.OperatorID) *ssv.SignedPartialSignatureMessage {
	return randaoMsg(sk, id, false, testingutils.TestingDutyEpoch, testingutils.TestingDutySlot+1, 1)
}

var PreConsensusRandaoMultiMsg = func(sk *bls.SecretKey, id types.OperatorID) *ssv.SignedPartialSignatureMessage {
	return randaoMsg(sk, id, false, testingutils.TestingDutyEpoch, testingutils.TestingDutySlot, 2)
}

var PreConsensusRandaoNoMsg = func(sk *bls.SecretKey, id types.OperatorID) *ssv.SignedPartialSignatureMessage {
	return randaoMsg(sk, id, false, testingutils.TestingDutyEpoch, testingutils.TestingDutySlot, 0)
}

var PreConsensusRandaoDifferentSignerMsg = func(msgSigner, randaoSigner *bls.SecretKey, msgSignerID, randaoSignerID types.OperatorID) *ssv.SignedPartialSignatureMessage {
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(testingutils.TestingDutyEpoch, types.DomainRandao)
	signed, root, _ := signer.SignBeaconObject(types.SSZUint64(testingutils.TestingDutyEpoch), d, randaoSigner.GetPublicKey().Serialize())

	msg := ssv.PartialSignatureMessages{
		Type: ssv.RandaoPartialSig,
		Messages: []*ssv.PartialSignatureMessage{
			{
				Slot:             testingutils.TestingDutySlot,
				PartialSignature: signed[:],
				SigningRoot:      root,
				Signer:           randaoSignerID,
			},
		},
	}
	sig, _ := signer.SignRoot(msg, types.PartialSignatureType, msgSigner.GetPublicKey().Serialize())
	return &ssv.SignedPartialSignatureMessage{
		Message:   msg,
		Signature: sig,
		Signer:    msgSignerID,
	}
}

var randaoMsg = func(
	sk *bls.SecretKey,
	id types.OperatorID,
	wrongRoot bool,
	epoch spec.Epoch,
	slot spec.Slot,
	msgCnt int,
) *ssv.SignedPartialSignatureMessage {
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(epoch, types.DomainRandao)
	signed, root, _ := signer.SignBeaconObject(types.SSZUint64(epoch), d, sk.GetPublicKey().Serialize())

	msgs := ssv.PartialSignatureMessages{
		Type:     ssv.RandaoPartialSig,
		Messages: []*ssv.PartialSignatureMessage{},
	}
	for i := 0; i < msgCnt; i++ {
		msg := &ssv.PartialSignatureMessage{
			Slot:             slot,
			PartialSignature: signed[:],
			SigningRoot:      root,
			Signer:           id,
		}
		if wrongRoot {
			msg.SigningRoot = make([]byte, 32)
		}
		msgs.Messages = append(msgs.Messages, msg)
	}

	sig, _ := signer.SignRoot(msgs, types.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &ssv.SignedPartialSignatureMessage{
		Message:   msgs,
		Signature: sig,
		Signer:    id,
	}
}

var PreConsensusSelectionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID types.OperatorID) *ssv.SignedPartialSignatureMessage {
	return PreConsensusCustomSlotSelectionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot)
}

var PreConsensusSelectionProofNextEpochMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID types.OperatorID) *ssv.SignedPartialSignatureMessage {
	return selectionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot2, testingutils.TestingDutySlot2, 1)
}

var PreConsensusMultiSelectionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID types.OperatorID) *ssv.SignedPartialSignatureMessage {
	return selectionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot, testingutils.TestingDutySlot, 3)
}

var PreConsensusCustomSlotSelectionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID types.OperatorID, slot spec.Slot) *ssv.SignedPartialSignatureMessage {
	return selectionProofMsg(msgSK, beaconSK, msgID, beaconID, slot, testingutils.TestingDutySlot, 1)
}

var PreConsensusWrongMsgSlotSelectionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID types.OperatorID) *ssv.SignedPartialSignatureMessage {
	return selectionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot, testingutils.TestingDutySlot+1, 1)
}

var selectionProofMsg = func(
	sk *bls.SecretKey,
	beaconsk *bls.SecretKey,
	id types.OperatorID,
	beaconid types.OperatorID,
	slot spec.Slot,
	msgSlot spec.Slot,
	msgCnt int,
) *ssv.SignedPartialSignatureMessage {
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(1, types.DomainSelectionProof)
	signed, root, _ := signer.SignBeaconObject(types.SSZUint64(slot), d, beaconsk.GetPublicKey().Serialize())

	_msgs := make([]*ssv.PartialSignatureMessage, 0)
	for i := 0; i < msgCnt; i++ {
		_msgs = append(_msgs, &ssv.PartialSignatureMessage{
			Slot:             msgSlot,
			PartialSignature: signed[:],
			SigningRoot:      root,
			Signer:           beaconid,
		})
	}

	msgs := ssv.PartialSignatureMessages{
		Type:     ssv.SelectionProofPartialSig,
		Messages: _msgs,
	}
	msgSig, _ := signer.SignRoot(msgs, types.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &ssv.SignedPartialSignatureMessage{
		Message:   msgs,
		Signature: msgSig,
		Signer:    id,
	}
}

var PostConsensusAggregatorMsg = func(sk *bls.SecretKey, id types.OperatorID) *ssv.SignedPartialSignatureMessage {
	return postConsensusAggregatorMsg(sk, id, false, false)
}

var postConsensusAggregatorMsg = func(
	sk *bls.SecretKey,
	id types.OperatorID,
	wrongRoot bool,
	wrongBeaconSig bool,
) *ssv.SignedPartialSignatureMessage {
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(1, types.DomainAggregateAndProof)
	signed, root, _ := signer.SignBeaconObject(testingutils.TestingAggregateAndProof, d, sk.GetPublicKey().Serialize())

	if wrongBeaconSig {
		//signed, _, _ = signer.SignAttestation(testingutils.TestingAttestationData, testingutils.TestingAttesterDuty, testingutils.TestingWrongSK.GetPublicKey().Serialize())
		panic("implement")
	}

	if wrongRoot {
		root = []byte{1, 2, 3, 4}
	}

	msgs := ssv.PartialSignatureMessages{
		Type: ssv.PostConsensusPartialSig,
		Messages: []*ssv.PartialSignatureMessage{
			{
				Slot:             testingutils.TestingDutySlot,
				PartialSignature: signed,
				SigningRoot:      root,
				Signer:           id,
			},
		},
	}
	sig, _ := signer.SignRoot(msgs, types.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &ssv.SignedPartialSignatureMessage{
		Message:   msgs,
		Signature: sig,
		Signer:    id,
	}
}

var PostConsensusSyncCommitteeMsg = func(sk *bls.SecretKey, id types.OperatorID) *ssv.SignedPartialSignatureMessage {
	return postConsensusSyncCommitteeMsg(sk, id, false, false)
}

var postConsensusSyncCommitteeMsg = func(
	sk *bls.SecretKey,
	id types.OperatorID,
	wrongRoot bool,
	wrongBeaconSig bool,
) *ssv.SignedPartialSignatureMessage {
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(1, types.DomainSyncCommittee)
	signed, root, _ := signer.SignBeaconObject(types.SSZBytes(testingutils.TestingSyncCommitteeBlockRoot[:]), d, sk.GetPublicKey().Serialize())

	if wrongBeaconSig {
		//signedAtt, _, _ = signer.SignAttestation(testingutils.TestingAttestationData, testingutils.TestingAttesterDuty, testingutils.TestingWrongSK.GetPublicKey().Serialize())
		panic("implement")
	}

	if wrongRoot {
		root = []byte{1, 2, 3, 4}
	}

	msgs := ssv.PartialSignatureMessages{
		Type: ssv.PostConsensusPartialSig,
		Messages: []*ssv.PartialSignatureMessage{
			{
				Slot:             testingutils.TestingDutySlot,
				PartialSignature: signed,
				SigningRoot:      root,
				Signer:           id,
			},
		},
	}
	sig, _ := signer.SignRoot(msgs, types.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &ssv.SignedPartialSignatureMessage{
		Message:   msgs,
		Signature: sig,
		Signer:    id,
	}
}

var PreConsensusContributionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID types.OperatorID) *ssv.SignedPartialSignatureMessage {
	return PreConsensusCustomSlotContributionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot)
}

var PreConsensusContributionProofNextEpochMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID types.OperatorID) *ssv.SignedPartialSignatureMessage {
	return contributionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot2, testingutils.TestingDutySlot2, false, false)
}

var PreConsensusCustomSlotContributionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID types.OperatorID, slot spec.Slot) *ssv.SignedPartialSignatureMessage {
	return contributionProofMsg(msgSK, beaconSK, msgID, beaconID, slot, testingutils.TestingDutySlot, false, false)
}

var PreConsensusWrongMsgSlotContributionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID types.OperatorID) *ssv.SignedPartialSignatureMessage {
	return contributionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot, testingutils.TestingDutySlot+1, false, false)
}

var PreConsensusWrongOrderContributionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID types.OperatorID) *ssv.SignedPartialSignatureMessage {
	return contributionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot, testingutils.TestingDutySlot, true, false)
}

var PreConsensusWrongCountContributionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID types.OperatorID) *ssv.SignedPartialSignatureMessage {
	return contributionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot, testingutils.TestingDutySlot, false, true)
}

var contributionProofMsg = func(
	sk, beaconsk *bls.SecretKey,
	id, beaconid types.OperatorID,
	slot spec.Slot,
	msgSlot spec.Slot,
	wrongMsgOrder bool,
	dropLastMsg bool,
) *ssv.SignedPartialSignatureMessage {
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(1, types.DomainSyncCommitteeSelectionProof)

	msgs := make([]*ssv.PartialSignatureMessage, 0)
	for index := range testingutils.TestingContributionProofIndexes {
		subnet, _ := beacon.SyncCommitteeSubnetID(uint64(index))
		data := &altair.SyncAggregatorSelectionData{
			Slot:              slot,
			SubcommitteeIndex: subnet,
		}
		sig, root, _ := signer.SignBeaconObject(data, d, beaconsk.GetPublicKey().Serialize())
		msg := &ssv.PartialSignatureMessage{
			Slot:             msgSlot,
			PartialSignature: sig[:],
			SigningRoot:      ensureRoot(root),
			Signer:           beaconid,
		}

		if dropLastMsg && index == len(testingutils.TestingContributionProofIndexes)-1 {
			break
		}
		msgs = append(msgs, msg)
	}

	if wrongMsgOrder {
		m := msgs[0]
		msgs[0] = msgs[1]
		msgs[1] = m
	}

	msg := &ssv.PartialSignatureMessages{
		Type:     ssv.ContributionProofs,
		Messages: msgs,
	}

	msgSig, _ := signer.SignRoot(msg, types.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &ssv.SignedPartialSignatureMessage{
		Message:   *msg,
		Signature: msgSig,
		Signer:    id,
	}
}

var PostConsensusSyncCommitteeContributionMsg = func(sk *bls.SecretKey, id types.OperatorID, keySet *testingutils.TestKeySet) *ssv.SignedPartialSignatureMessage {
	return postConsensusSyncCommitteeContributionMsg(sk, id, testingutils.TestingValidatorIndex, keySet, false, false)
}

var postConsensusSyncCommitteeContributionMsg = func(
	sk *bls.SecretKey,
	id types.OperatorID,
	validatorIndex spec.ValidatorIndex,
	keySet *testingutils.TestKeySet,
	wrongRoot bool,
	wrongBeaconSig bool,
) *ssv.SignedPartialSignatureMessage {
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()
	dContribAndProof, _ := beacon.DomainData(1, types.DomainContributionAndProof)

	msgs := make([]*ssv.PartialSignatureMessage, 0)
	for index := range testingutils.TestingSyncCommitteeContributions {
		// sign proof
		subnet, _ := beacon.SyncCommitteeSubnetID(uint64(index))
		data := &altair.SyncAggregatorSelectionData{
			Slot:              testingutils.TestingDutySlot,
			SubcommitteeIndex: subnet,
		}
		dProof, _ := beacon.DomainData(1, types.DomainSyncCommitteeSelectionProof)

		proofSig, _, _ := signer.SignBeaconObject(data, dProof, keySet.ValidatorPK.Serialize())
		blsProofSig := spec.BLSSignature{}
		copy(blsProofSig[:], proofSig)

		// get contribution
		contribution, _ := beacon.GetSyncCommitteeContribution(testingutils.TestingDutySlot, subnet, testingutils.TestingValidatorPubKey)

		// sign contrib and proof
		contribAndProof := &altair.ContributionAndProof{
			AggregatorIndex: validatorIndex,
			Contribution:    contribution,
			SelectionProof:  blsProofSig,
		}
		signed, root, _ := signer.SignBeaconObject(contribAndProof, dContribAndProof, sk.GetPublicKey().Serialize())

		if wrongRoot {
			root = []byte{1, 2, 3, 4}
		}

		msg := &ssv.PartialSignatureMessage{
			Slot:             testingutils.TestingDutySlot,
			PartialSignature: signed,
			SigningRoot:      root,
			Signer:           id,
		}

		if wrongBeaconSig {
			//signedAtt, _, _ = signer.SignAttestation(testingutils.TestingAttestationData, testingutils.TestingAttesterDuty, testingutils.TestingWrongSK.GetPublicKey().Serialize())
			panic("implement")
		}

		msgs = append(msgs, msg)
	}

	msg := &ssv.PartialSignatureMessages{
		Type:     ssv.PostConsensusPartialSig,
		Messages: msgs,
	}

	sig, _ := signer.SignRoot(msg, types.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &ssv.SignedPartialSignatureMessage{
		Message:   *msg,
		Signature: sig,
		Signer:    id,
	}
}

// ensureRoot ensures that SigningRoot will have sufficient allocated memory
// otherwise we get panic from bls:
// github.com/herumi/bls-eth-go-binary/bls.(*Sign).VerifyByte:738
func ensureRoot(root []byte) []byte {
	n := len(root)
	if n == 0 {
		n = 1
	}
	tmp := make([]byte, n)
	copy(tmp[:], root[:])
	return tmp[:]
}
