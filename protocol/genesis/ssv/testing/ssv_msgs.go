package testing

import (
	spec2 "github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	"github.com/ssvlabs/ssv-spec-pre-cc/types/testingutils"
)

var TestingSSVDomainType = genesisspectypes.JatoTestnet
var AttesterMsgID = func() []byte {
	ret := genesisspectypes.NewMsgID(TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], genesisspectypes.BNRoleAttester)
	return ret[:]
}()

var ProposerMsgID = func() []byte {
	ret := genesisspectypes.NewMsgID(TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], genesisspectypes.BNRoleProposer)
	return ret[:]
}()
var AggregatorMsgID = func() []byte {
	ret := genesisspectypes.NewMsgID(TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], genesisspectypes.BNRoleAggregator)
	return ret[:]
}()
var SyncCommitteeMsgID = func() []byte {
	ret := genesisspectypes.NewMsgID(TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], genesisspectypes.BNRoleSyncCommittee)
	return ret[:]
}()
var SyncCommitteeContributionMsgID = func() []byte {
	ret := genesisspectypes.NewMsgID(TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], genesisspectypes.BNRoleSyncCommitteeContribution)
	return ret[:]
}()
var ValidatorRegistrationMsgID = func() []byte {
	ret := genesisspectypes.NewMsgID(TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], genesisspectypes.BNRoleValidatorRegistration)
	return ret[:]
}()

var TestAttesterConsensusData = &genesisspectypes.ConsensusData{
	Duty:    testingutils.TestingAttesterDuty,
	DataSSZ: testingutils.TestingAttestationDataBytes,
}
var TestAttesterConsensusDataByts, _ = TestAttesterConsensusData.Encode()

var TestAggregatorConsensusData = &genesisspectypes.ConsensusData{
	Duty:    testingutils.TestingAggregatorDuty,
	DataSSZ: testingutils.TestingAggregateAndProofBytes,
}
var TestAggregatorConsensusDataByts, _ = TestAggregatorConsensusData.Encode()

var TestProposerBlindedBlockConsensusData = &genesisspectypes.ConsensusData{
	Duty:    *testingutils.TestingProposerDutyV(spec2.DataVersionCapella),
	Version: spec2.DataVersionCapella,
	DataSSZ: testingutils.TestingBlindedBeaconBlockBytesV(spec2.DataVersionCapella),
}
var TestProposerBlindedBlockConsensusDataByts, _ = TestProposerBlindedBlockConsensusData.Encode()

var TestSyncCommitteeConsensusData = &genesisspectypes.ConsensusData{
	Duty:    testingutils.TestingSyncCommitteeDuty,
	DataSSZ: testingutils.TestingSyncCommitteeBlockRoot[:],
}
var TestSyncCommitteeConsensusDataByts, _ = TestSyncCommitteeConsensusData.Encode()

var TestSyncCommitteeContributionConsensusData = &genesisspectypes.ConsensusData{
	Duty:    testingutils.TestingSyncCommitteeContributionDuty,
	DataSSZ: testingutils.TestingContributionsDataBytes,
}
var TestSyncCommitteeContributionConsensusDataByts, _ = TestSyncCommitteeContributionConsensusData.Encode()

var TestConsensusUnkownDutyTypeData = &genesisspectypes.ConsensusData{
	Duty:    testingutils.TestingUnknownDutyType,
	DataSSZ: testingutils.TestingAttestationDataBytes,
}
var TestConsensusUnkownDutyTypeDataByts, _ = TestConsensusUnkownDutyTypeData.Encode()

var TestConsensusWrongDutyPKData = &genesisspectypes.ConsensusData{
	Duty:    testingutils.TestingWrongDutyPK,
	DataSSZ: testingutils.TestingAttestationDataBytes,
}
var TestConsensusWrongDutyPKDataByts, _ = TestConsensusWrongDutyPKData.Encode()

var SSVMsgAttester = func(qbftMsg *genesisspecqbft.SignedMessage, partialSigMsg *genesisspectypes.SignedPartialSignatureMessage) *genesisspectypes.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, genesisspectypes.NewMsgID(TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], genesisspectypes.BNRoleAttester))
}

var SSVMsgWrongID = func(qbftMsg *genesisspecqbft.SignedMessage, partialSigMsg *genesisspectypes.SignedPartialSignatureMessage) *genesisspectypes.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, genesisspectypes.NewMsgID(TestingSSVDomainType, testingutils.TestingWrongValidatorPubKey[:], genesisspectypes.BNRoleAttester))
}

var SSVMsgProposer = func(qbftMsg *genesisspecqbft.SignedMessage, partialSigMsg *genesisspectypes.SignedPartialSignatureMessage) *genesisspectypes.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, genesisspectypes.NewMsgID(TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], genesisspectypes.BNRoleProposer))
}

var SSVMsgAggregator = func(qbftMsg *genesisspecqbft.SignedMessage, partialSigMsg *genesisspectypes.SignedPartialSignatureMessage) *genesisspectypes.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, genesisspectypes.NewMsgID(TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], genesisspectypes.BNRoleAggregator))
}

var SSVMsgSyncCommittee = func(qbftMsg *genesisspecqbft.SignedMessage, partialSigMsg *genesisspectypes.SignedPartialSignatureMessage) *genesisspectypes.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, genesisspectypes.NewMsgID(TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], genesisspectypes.BNRoleSyncCommittee))
}

var SSVMsgSyncCommitteeContribution = func(qbftMsg *genesisspecqbft.SignedMessage, partialSigMsg *genesisspectypes.SignedPartialSignatureMessage) *genesisspectypes.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, genesisspectypes.NewMsgID(TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], genesisspectypes.BNRoleSyncCommitteeContribution))
}

var SSVMsgValidatorRegistration = func(qbftMsg *genesisspecqbft.SignedMessage, partialSigMsg *genesisspectypes.SignedPartialSignatureMessage) *genesisspectypes.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, genesisspectypes.NewMsgID(TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], genesisspectypes.BNRoleValidatorRegistration))
}

var ssvMsg = func(qbftMsg *genesisspecqbft.SignedMessage, postMsg *genesisspectypes.SignedPartialSignatureMessage, msgID genesisspectypes.MessageID) *genesisspectypes.SSVMessage {
	var msgType genesisspectypes.MsgType
	var data []byte
	var err error
	if qbftMsg != nil {
		msgType = genesisspectypes.SSVConsensusMsgType
		data, err = qbftMsg.Encode()
		if err != nil {
			panic(err)
		}
	} else if postMsg != nil {
		msgType = genesisspectypes.SSVPartialSignatureMsgType
		data, err = postMsg.Encode()
		if err != nil {
			panic(err)
		}
	} else {
		panic("msg type undefined")
	}

	return &genesisspectypes.SSVMessage{
		MsgType: msgType,
		MsgID:   msgID,
		Data:    data,
	}
}

var PostConsensusWrongAttestationMsg = func(sk *bls.SecretKey, id genesisspectypes.OperatorID, height genesisspecqbft.Height) *genesisspectypes.SignedPartialSignatureMessage {
	return postConsensusAttestationMsg(sk, id, height, true, false)
}

var PostConsensusWrongSigAttestationMsg = func(sk *bls.SecretKey, id genesisspectypes.OperatorID, height genesisspecqbft.Height) *genesisspectypes.SignedPartialSignatureMessage {
	return postConsensusAttestationMsg(sk, id, height, false, true)
}

var PostConsensusSigAttestationWrongBeaconSignerMsg = func(sk *bls.SecretKey, id, beaconSigner genesisspectypes.OperatorID, height genesisspecqbft.Height) *genesisspectypes.SignedPartialSignatureMessage {
	ret := postConsensusAttestationMsg(sk, beaconSigner, height, false, true)
	ret.Signer = id
	return ret
}

var PostConsensusAttestationMsg = func(sk *bls.SecretKey, id genesisspectypes.OperatorID, height genesisspecqbft.Height) *genesisspectypes.SignedPartialSignatureMessage {
	return postConsensusAttestationMsg(sk, id, height, false, false)
}

var PostConsensusAttestationTooManyRootsMsg = func(sk *bls.SecretKey, id genesisspectypes.OperatorID, height genesisspecqbft.Height) *genesisspectypes.SignedPartialSignatureMessage {
	ret := postConsensusAttestationMsg(sk, id, height, false, false)
	ret.Message.Messages = append(ret.Message.Messages, ret.Message.Messages[0])

	msg := &genesisspectypes.PartialSignatureMessages{
		Type:     genesisspectypes.PostConsensusPartialSig,
		Slot:     testingutils.TestingDutySlot,
		Messages: ret.Message.Messages,
	}

	sig, _ := testingutils.NewTestingKeyManager().SignRoot(msg, genesisspectypes.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &genesisspectypes.SignedPartialSignatureMessage{
		Message:   *msg,
		Signature: sig,
		Signer:    id,
	}
}

var PostConsensusAttestationTooFewRootsMsg = func(sk *bls.SecretKey, id genesisspectypes.OperatorID, height genesisspecqbft.Height) *genesisspectypes.SignedPartialSignatureMessage {
	msg := &genesisspectypes.PartialSignatureMessages{
		Type:     genesisspectypes.PostConsensusPartialSig,
		Slot:     testingutils.TestingDutySlot,
		Messages: []*genesisspectypes.PartialSignatureMessage{},
	}

	sig, _ := testingutils.NewTestingKeyManager().SignRoot(msg, genesisspectypes.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &genesisspectypes.SignedPartialSignatureMessage{
		Message:   *msg,
		Signature: sig,
		Signer:    id,
	}
}

var postConsensusAttestationMsg = func(
	sk *bls.SecretKey,
	id genesisspectypes.OperatorID,
	height genesisspecqbft.Height,
	wrongRoot bool,
	wrongBeaconSig bool,
) *genesisspectypes.SignedPartialSignatureMessage {
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(testingutils.TestingAttestationData.Target.Epoch, genesisspectypes.DomainAttester)

	attData := testingutils.TestingAttestationData
	if wrongRoot {
		attData = testingutils.TestingWrongAttestationData
	}

	signed, root, _ := signer.SignBeaconObject(attData, d, sk.GetPublicKey().Serialize(), genesisspectypes.DomainAttester)

	if wrongBeaconSig {
		signed, _, _ = signer.SignBeaconObject(attData, d, testingutils.Testing7SharesSet().ValidatorPK.Serialize(), genesisspectypes.DomainAttester)
	}

	msgs := genesisspectypes.PartialSignatureMessages{
		Type: genesisspectypes.PostConsensusPartialSig,
		Slot: testingutils.TestingDutySlot,
		Messages: []*genesisspectypes.PartialSignatureMessage{
			{
				PartialSignature: signed,
				SigningRoot:      root,
				Signer:           id,
			},
		},
	}
	sig, _ := signer.SignRoot(msgs, genesisspectypes.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &genesisspectypes.SignedPartialSignatureMessage{
		Message:   msgs,
		Signature: sig,
		Signer:    id,
	}
}

var PostConsensusProposerMsg = func(sk *bls.SecretKey, id genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return postConsensusBeaconBlockMsg(sk, id, false, false)
}

var PostConsensusProposerTooManyRootsMsg = func(sk *bls.SecretKey, id genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	ret := postConsensusBeaconBlockMsg(sk, id, false, false)
	ret.Message.Messages = append(ret.Message.Messages, ret.Message.Messages[0])

	msg := &genesisspectypes.PartialSignatureMessages{
		Type:     genesisspectypes.PostConsensusPartialSig,
		Slot:     testingutils.TestingDutySlot,
		Messages: ret.Message.Messages,
	}

	sig, _ := testingutils.NewTestingKeyManager().SignRoot(msg, genesisspectypes.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &genesisspectypes.SignedPartialSignatureMessage{
		Message:   *msg,
		Signature: sig,
		Signer:    id,
	}
}

var PostConsensusProposerTooFewRootsMsg = func(sk *bls.SecretKey, id genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	msg := &genesisspectypes.PartialSignatureMessages{
		Type:     genesisspectypes.PostConsensusPartialSig,
		Slot:     testingutils.TestingDutySlot,
		Messages: []*genesisspectypes.PartialSignatureMessage{},
	}

	sig, _ := testingutils.NewTestingKeyManager().SignRoot(msg, genesisspectypes.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &genesisspectypes.SignedPartialSignatureMessage{
		Message:   *msg,
		Signature: sig,
		Signer:    id,
	}
}

var PostConsensusWrongProposerMsg = func(sk *bls.SecretKey, id genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return postConsensusBeaconBlockMsg(sk, id, true, false)
}

var PostConsensusWrongSigProposerMsg = func(sk *bls.SecretKey, id genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return postConsensusBeaconBlockMsg(sk, id, false, true)
}

var PostConsensusSigProposerWrongBeaconSignerMsg = func(sk *bls.SecretKey, id, beaconSigner genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	ret := postConsensusBeaconBlockMsg(sk, beaconSigner, false, true)
	ret.Signer = id
	return ret
}

var postConsensusBeaconBlockMsg = func(
	sk *bls.SecretKey,
	id genesisspectypes.OperatorID,
	wrongRoot bool,
	wrongBeaconSig bool,
) *genesisspectypes.SignedPartialSignatureMessage {
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()

	block := testingutils.TestingBeaconBlockV(spec2.DataVersionDeneb).Deneb
	if wrongRoot {
		block = testingutils.TestingWrongBeaconBlockV(spec2.DataVersionDeneb).Deneb
	}

	d, _ := beacon.DomainData(1, genesisspectypes.DomainProposer) // epoch doesn't matter here, hard coded
	sig, root, _ := signer.SignBeaconObject(block, d, sk.GetPublicKey().Serialize(), genesisspectypes.DomainProposer)
	if wrongBeaconSig {
		sig, root, _ = signer.SignBeaconObject(block, d, testingutils.Testing7SharesSet().ValidatorPK.Serialize(), genesisspectypes.DomainProposer)
	}
	blsSig := spec.BLSSignature{}
	copy(blsSig[:], sig)

	signed := deneb.SignedBeaconBlock{
		Message:   block.Block,
		Signature: blsSig,
	}

	msgs := genesisspectypes.PartialSignatureMessages{
		Type: genesisspectypes.PostConsensusPartialSig,
		Slot: testingutils.TestingDutySlot,
		Messages: []*genesisspectypes.PartialSignatureMessage{
			{
				PartialSignature: signed.Signature[:],
				SigningRoot:      root,
				Signer:           id,
			},
		},
	}
	msgSig, _ := signer.SignRoot(msgs, genesisspectypes.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &genesisspectypes.SignedPartialSignatureMessage{
		Message:   msgs,
		Signature: msgSig,
		Signer:    id,
	}
}

var PreConsensusFailedMsg = func(msgSigner *bls.SecretKey, msgSignerID genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(testingutils.TestingDutyEpoch, genesisspectypes.DomainRandao)
	signed, root, _ := signer.SignBeaconObject(genesisspectypes.SSZUint64(testingutils.TestingDutyEpoch), d, msgSigner.GetPublicKey().Serialize(), genesisspectypes.DomainRandao)

	msg := genesisspectypes.PartialSignatureMessages{
		Type: genesisspectypes.RandaoPartialSig,
		Slot: testingutils.TestingDutySlot,
		Messages: []*genesisspectypes.PartialSignatureMessage{
			{
				PartialSignature: signed[:],
				SigningRoot:      root,
				Signer:           msgSignerID,
			},
		},
	}
	sig, _ := signer.SignRoot(msg, genesisspectypes.PartialSignatureType, msgSigner.GetPublicKey().Serialize())
	return &genesisspectypes.SignedPartialSignatureMessage{
		Message:   msg,
		Signature: sig,
		Signer:    msgSignerID,
	}
}

var PreConsensusRandaoMsg = func(sk *bls.SecretKey, id genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return randaoMsg(sk, id, false, testingutils.TestingDutyEpoch, 1, false)
}

// PreConsensusRandaoNextEpochMsg testing for a second duty start
var PreConsensusRandaoNextEpochMsg = func(sk *bls.SecretKey, id genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return randaoMsg(sk, id, false, testingutils.TestingDutyEpoch+1, 1, false)
}

var PreConsensusRandaoDifferentEpochMsg = func(sk *bls.SecretKey, id genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return randaoMsg(sk, id, false, testingutils.TestingDutyEpoch+1, 1, false)
}

var PreConsensusRandaoTooManyRootsMsg = func(sk *bls.SecretKey, id genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return randaoMsg(sk, id, false, testingutils.TestingDutyEpoch, 2, false)
}

var PreConsensusRandaoTooFewRootsMsg = func(sk *bls.SecretKey, id genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return randaoMsg(sk, id, false, testingutils.TestingDutyEpoch, 0, false)
}

var PreConsensusRandaoNoMsg = func(sk *bls.SecretKey, id genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return randaoMsg(sk, id, false, testingutils.TestingDutyEpoch, 0, false)
}

var PreConsensusRandaoWrongBeaconSigMsg = func(sk *bls.SecretKey, id genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return randaoMsg(sk, id, false, testingutils.TestingDutyEpoch, 1, true)
}

var PreConsensusRandaoDifferentSignerMsg = func(
	msgSigner, randaoSigner *bls.SecretKey,
	msgSignerID,
	randaoSignerID genesisspectypes.OperatorID,
) *genesisspectypes.SignedPartialSignatureMessage {
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(testingutils.TestingDutyEpoch, genesisspectypes.DomainRandao)
	signed, root, _ := signer.SignBeaconObject(genesisspectypes.SSZUint64(testingutils.TestingDutyEpoch), d, randaoSigner.GetPublicKey().Serialize(), genesisspectypes.DomainRandao)

	msg := genesisspectypes.PartialSignatureMessages{
		Type: genesisspectypes.RandaoPartialSig,
		Slot: testingutils.TestingDutySlot,
		Messages: []*genesisspectypes.PartialSignatureMessage{
			{
				PartialSignature: signed[:],
				SigningRoot:      root,
				Signer:           randaoSignerID,
			},
		},
	}
	sig, _ := signer.SignRoot(msg, genesisspectypes.PartialSignatureType, msgSigner.GetPublicKey().Serialize())
	return &genesisspectypes.SignedPartialSignatureMessage{
		Message:   msg,
		Signature: sig,
		Signer:    msgSignerID,
	}
}

var randaoMsg = func(
	sk *bls.SecretKey,
	id genesisspectypes.OperatorID,
	wrongRoot bool,
	epoch spec.Epoch,
	msgCnt int,
	wrongBeaconSig bool,
) *genesisspectypes.SignedPartialSignatureMessage {
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(epoch, genesisspectypes.DomainRandao)
	signed, root, _ := signer.SignBeaconObject(genesisspectypes.SSZUint64(epoch), d, sk.GetPublicKey().Serialize(), genesisspectypes.DomainRandao)
	if wrongBeaconSig {
		signed, root, _ = signer.SignBeaconObject(genesisspectypes.SSZUint64(testingutils.TestingDutyEpoch), d, testingutils.Testing7SharesSet().ValidatorPK.Serialize(), genesisspectypes.DomainRandao)
	}

	msgs := genesisspectypes.PartialSignatureMessages{
		Type:     genesisspectypes.RandaoPartialSig,
		Slot:     testingutils.TestingDutySlot,
		Messages: []*genesisspectypes.PartialSignatureMessage{},
	}
	for i := 0; i < msgCnt; i++ {
		msg := &genesisspectypes.PartialSignatureMessage{
			PartialSignature: signed[:],
			SigningRoot:      root,
			Signer:           id,
		}
		if wrongRoot {
			msg.SigningRoot = [32]byte{}
		}
		msgs.Messages = append(msgs.Messages, msg)
	}

	sig, _ := signer.SignRoot(msgs, genesisspectypes.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &genesisspectypes.SignedPartialSignatureMessage{
		Message:   msgs,
		Signature: sig,
		Signer:    id,
	}
}

var PreConsensusSelectionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return PreConsensusCustomSlotSelectionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot)
}

var PreConsensusSelectionProofWrongBeaconSigMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return selectionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot, testingutils.TestingDutySlot, 1, true)
}

var PreConsensusSelectionProofNextEpochMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return selectionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot2, testingutils.TestingDutySlot2, 1, false)
}

var PreConsensusSelectionProofTooManyRootsMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return selectionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot, testingutils.TestingDutySlot, 3, false)
}

var PreConsensusSelectionProofTooFewRootsMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return selectionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot, testingutils.TestingDutySlot, 0, false)
}

var PreConsensusCustomSlotSelectionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID genesisspectypes.OperatorID, slot spec.Slot) *genesisspectypes.SignedPartialSignatureMessage {
	return selectionProofMsg(msgSK, beaconSK, msgID, beaconID, slot, testingutils.TestingDutySlot, 1, false)
}

var PreConsensusWrongMsgSlotSelectionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return selectionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot, testingutils.TestingDutySlot+1, 1, false)
}

var selectionProofMsg = func(
	sk *bls.SecretKey,
	beaconsk *bls.SecretKey,
	id genesisspectypes.OperatorID,
	beaconid genesisspectypes.OperatorID,
	slot spec.Slot,
	msgSlot spec.Slot,
	msgCnt int,
	wrongBeaconSig bool,
) *genesisspectypes.SignedPartialSignatureMessage {
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(1, genesisspectypes.DomainSelectionProof)
	signed, root, _ := signer.SignBeaconObject(genesisspectypes.SSZUint64(slot), d, beaconsk.GetPublicKey().Serialize(), genesisspectypes.DomainSelectionProof)
	if wrongBeaconSig {
		signed, root, _ = signer.SignBeaconObject(genesisspectypes.SSZUint64(slot), d, testingutils.Testing7SharesSet().ValidatorPK.Serialize(), genesisspectypes.DomainSelectionProof)
	}

	_msgs := make([]*genesisspectypes.PartialSignatureMessage, 0)
	for i := 0; i < msgCnt; i++ {
		_msgs = append(_msgs, &genesisspectypes.PartialSignatureMessage{
			PartialSignature: signed[:],
			SigningRoot:      root,
			Signer:           beaconid,
		})
	}

	msgs := genesisspectypes.PartialSignatureMessages{
		Type:     genesisspectypes.SelectionProofPartialSig,
		Slot:     testingutils.TestingDutySlot,
		Messages: _msgs,
	}
	msgSig, _ := signer.SignRoot(msgs, genesisspectypes.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &genesisspectypes.SignedPartialSignatureMessage{
		Message:   msgs,
		Signature: msgSig,
		Signer:    id,
	}
}

var PreConsensusValidatorRegistrationMsg = func(msgSK *bls.SecretKey, msgID genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return validatorRegistrationMsg(msgSK, msgSK, msgID, msgID, 1, false, testingutils.TestingDutyEpoch, false)
}

var PreConsensusValidatorRegistrationTooFewRootsMsg = func(msgSK *bls.SecretKey, msgID genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return validatorRegistrationMsg(msgSK, msgSK, msgID, msgID, 0, false, testingutils.TestingDutyEpoch, false)
}

var PreConsensusValidatorRegistrationTooManyRootsMsg = func(msgSK *bls.SecretKey, msgID genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return validatorRegistrationMsg(msgSK, msgSK, msgID, msgID, 2, false, testingutils.TestingDutyEpoch, false)
}

var PreConsensusValidatorRegistrationDifferentEpochMsg = func(msgSK *bls.SecretKey, msgID genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return validatorRegistrationMsg(msgSK, msgSK, msgID, msgID, 1, true, testingutils.TestingDutyEpoch, false)
}

var validatorRegistrationMsg = func(
	sk, beaconSK *bls.SecretKey,
	id, beaconID genesisspectypes.OperatorID,
	msgCnt int,
	wrongRoot bool,
	epoch spec.Epoch,
	wrongBeaconSig bool,
) *genesisspectypes.SignedPartialSignatureMessage {
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(epoch, genesisspectypes.DomainApplicationBuilder)

	signed, root, _ := signer.SignBeaconObject(testingutils.TestingValidatorRegistration, d, beaconSK.GetPublicKey().Serialize(), genesisspectypes.DomainApplicationBuilder)
	if wrongRoot {
		signed, root, _ = signer.SignBeaconObject(testingutils.TestingValidatorRegistrationWrong, d, beaconSK.GetPublicKey().Serialize(), genesisspectypes.DomainApplicationBuilder)
	}
	if wrongBeaconSig {
		signed, root, _ = signer.SignBeaconObject(testingutils.TestingValidatorRegistration, d, testingutils.Testing7SharesSet().ValidatorPK.Serialize(), genesisspectypes.DomainApplicationBuilder)
	}

	msgs := genesisspectypes.PartialSignatureMessages{
		Type:     genesisspectypes.ValidatorRegistrationPartialSig,
		Slot:     testingutils.TestingDutySlot,
		Messages: []*genesisspectypes.PartialSignatureMessage{},
	}

	for i := 0; i < msgCnt; i++ {
		msg := &genesisspectypes.PartialSignatureMessage{
			PartialSignature: signed[:],
			SigningRoot:      root,
			Signer:           beaconID,
		}
		msgs.Messages = append(msgs.Messages, msg)
	}

	msg := &genesisspectypes.PartialSignatureMessage{
		PartialSignature: signed[:],
		SigningRoot:      root,
		Signer:           id,
	}
	if wrongRoot {
		msg.SigningRoot = [32]byte{}
	}

	sig, _ := signer.SignRoot(msgs, genesisspectypes.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &genesisspectypes.SignedPartialSignatureMessage{
		Message:   msgs,
		Signature: sig,
		Signer:    id,
	}
}

var PostConsensusAggregatorMsg = func(sk *bls.SecretKey, id genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return postConsensusAggregatorMsg(sk, id, false, false)
}

var PostConsensusAggregatorTooManyRootsMsg = func(sk *bls.SecretKey, id genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	ret := postConsensusAggregatorMsg(sk, id, false, false)
	ret.Message.Messages = append(ret.Message.Messages, ret.Message.Messages[0])

	msg := &genesisspectypes.PartialSignatureMessages{
		Type:     genesisspectypes.PostConsensusPartialSig,
		Slot:     testingutils.TestingDutySlot,
		Messages: ret.Message.Messages,
	}

	sig, _ := testingutils.NewTestingKeyManager().SignRoot(msg, genesisspectypes.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &genesisspectypes.SignedPartialSignatureMessage{
		Message:   *msg,
		Signature: sig,
		Signer:    id,
	}
}

var PostConsensusAggregatorTooFewRootsMsg = func(sk *bls.SecretKey, id genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	msg := &genesisspectypes.PartialSignatureMessages{
		Type:     genesisspectypes.PostConsensusPartialSig,
		Slot:     testingutils.TestingDutySlot,
		Messages: []*genesisspectypes.PartialSignatureMessage{},
	}

	sig, _ := testingutils.NewTestingKeyManager().SignRoot(msg, genesisspectypes.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &genesisspectypes.SignedPartialSignatureMessage{
		Message:   *msg,
		Signature: sig,
		Signer:    id,
	}
}

var PostConsensusWrongAggregatorMsg = func(sk *bls.SecretKey, id genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return postConsensusAggregatorMsg(sk, id, true, false)
}

var PostConsensusWrongSigAggregatorMsg = func(sk *bls.SecretKey, id genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return postConsensusAggregatorMsg(sk, id, false, true)
}

var PostConsensusSigAggregatorWrongBeaconSignerMsg = func(sk *bls.SecretKey, id, beaconSigner genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	ret := postConsensusAggregatorMsg(sk, beaconSigner, false, true)
	ret.Signer = id
	return ret
}

var postConsensusAggregatorMsg = func(
	sk *bls.SecretKey,
	id genesisspectypes.OperatorID,
	wrongRoot bool,
	wrongBeaconSig bool,
) *genesisspectypes.SignedPartialSignatureMessage {
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(1, genesisspectypes.DomainAggregateAndProof)

	aggData := testingutils.TestingAggregateAndProof
	if wrongRoot {
		aggData = testingutils.TestingWrongAggregateAndProof
	}

	signed, root, _ := signer.SignBeaconObject(aggData, d, sk.GetPublicKey().Serialize(), genesisspectypes.DomainAggregateAndProof)
	if wrongBeaconSig {
		signed, root, _ = signer.SignBeaconObject(aggData, d, testingutils.Testing7SharesSet().ValidatorPK.Serialize(), genesisspectypes.DomainAggregateAndProof)
	}

	msgs := genesisspectypes.PartialSignatureMessages{
		Type: genesisspectypes.PostConsensusPartialSig,
		Slot: testingutils.TestingDutySlot,
		Messages: []*genesisspectypes.PartialSignatureMessage{
			{
				PartialSignature: signed,
				SigningRoot:      root,
				Signer:           id,
			},
		},
	}
	sig, _ := signer.SignRoot(msgs, genesisspectypes.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &genesisspectypes.SignedPartialSignatureMessage{
		Message:   msgs,
		Signature: sig,
		Signer:    id,
	}
}

var PostConsensusSyncCommitteeMsg = func(sk *bls.SecretKey, id genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return postConsensusSyncCommitteeMsg(sk, id, false, false)
}

var PostConsensusSyncCommitteeTooManyRootsMsg = func(sk *bls.SecretKey, id genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	ret := postConsensusSyncCommitteeMsg(sk, id, false, false)
	ret.Message.Messages = append(ret.Message.Messages, ret.Message.Messages[0])

	msg := &genesisspectypes.PartialSignatureMessages{
		Type:     genesisspectypes.PostConsensusPartialSig,
		Slot:     testingutils.TestingDutySlot,
		Messages: ret.Message.Messages,
	}

	sig, _ := testingutils.NewTestingKeyManager().SignRoot(msg, genesisspectypes.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &genesisspectypes.SignedPartialSignatureMessage{
		Message:   *msg,
		Signature: sig,
		Signer:    id,
	}
}

var PostConsensusSyncCommitteeTooFewRootsMsg = func(sk *bls.SecretKey, id genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	msg := &genesisspectypes.PartialSignatureMessages{
		Type:     genesisspectypes.PostConsensusPartialSig,
		Slot:     testingutils.TestingDutySlot,
		Messages: []*genesisspectypes.PartialSignatureMessage{},
	}

	sig, _ := testingutils.NewTestingKeyManager().SignRoot(msg, genesisspectypes.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &genesisspectypes.SignedPartialSignatureMessage{
		Message:   *msg,
		Signature: sig,
		Signer:    id,
	}
}

var PostConsensusWrongSyncCommitteeMsg = func(sk *bls.SecretKey, id genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return postConsensusSyncCommitteeMsg(sk, id, true, false)
}

var PostConsensusWrongSigSyncCommitteeMsg = func(sk *bls.SecretKey, id genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return postConsensusSyncCommitteeMsg(sk, id, false, true)
}

var PostConsensusSigSyncCommitteeWrongBeaconSignerMsg = func(sk *bls.SecretKey, id, beaconSigner genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	ret := postConsensusSyncCommitteeMsg(sk, beaconSigner, false, true)
	ret.Signer = id
	return ret
}

var postConsensusSyncCommitteeMsg = func(
	sk *bls.SecretKey,
	id genesisspectypes.OperatorID,
	wrongRoot bool,
	wrongBeaconSig bool,
) *genesisspectypes.SignedPartialSignatureMessage {
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(1, genesisspectypes.DomainSyncCommittee)
	blockRoot := testingutils.TestingSyncCommitteeBlockRoot
	if wrongRoot {
		blockRoot = testingutils.TestingSyncCommitteeWrongBlockRoot
	}
	signed, root, _ := signer.SignBeaconObject(genesisspectypes.SSZBytes(blockRoot[:]), d, sk.GetPublicKey().Serialize(), genesisspectypes.DomainSyncCommittee)
	if wrongBeaconSig {
		signed, root, _ = signer.SignBeaconObject(genesisspectypes.SSZBytes(blockRoot[:]), d, testingutils.Testing7SharesSet().ValidatorPK.Serialize(), genesisspectypes.DomainSyncCommittee)
	}

	msgs := genesisspectypes.PartialSignatureMessages{
		Type: genesisspectypes.PostConsensusPartialSig,
		Slot: testingutils.TestingDutySlot,
		Messages: []*genesisspectypes.PartialSignatureMessage{
			{
				PartialSignature: signed,
				SigningRoot:      root,
				Signer:           id,
			},
		},
	}
	sig, _ := signer.SignRoot(msgs, genesisspectypes.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &genesisspectypes.SignedPartialSignatureMessage{
		Message:   msgs,
		Signature: sig,
		Signer:    id,
	}
}

var PreConsensusContributionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return PreConsensusCustomSlotContributionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot)
}

var PreConsensusContributionProofWrongBeaconSigMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return contributionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot, testingutils.TestingDutySlot+1, false, true)
}

var PreConsensusContributionProofNextEpochMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return contributionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot2, testingutils.TestingDutySlot2, false, false)
}

var PreConsensusCustomSlotContributionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID genesisspectypes.OperatorID, slot spec.Slot) *genesisspectypes.SignedPartialSignatureMessage {
	return contributionProofMsg(msgSK, beaconSK, msgID, beaconID, slot, testingutils.TestingDutySlot, false, false)
}

var PreConsensusWrongMsgSlotContributionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return contributionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot, testingutils.TestingDutySlot+1, false, false)
}

var PreConsensusWrongOrderContributionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	return contributionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot, testingutils.TestingDutySlot, true, false)
}

var PreConsensusContributionProofTooManyRootsMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	ret := contributionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot, testingutils.TestingDutySlot, false, false)
	msg := &genesisspectypes.PartialSignatureMessages{
		Type:     genesisspectypes.ContributionProofs,
		Slot:     testingutils.TestingDutySlot,
		Messages: append(ret.Message.Messages, ret.Message.Messages[0]),
	}

	msgSig, _ := testingutils.NewTestingKeyManager().SignRoot(msg, genesisspectypes.PartialSignatureType, beaconSK.GetPublicKey().Serialize())
	return &genesisspectypes.SignedPartialSignatureMessage{
		Message:   *msg,
		Signature: msgSig,
		Signer:    msgID,
	}
}

var PreConsensusContributionProofTooFewRootsMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID genesisspectypes.OperatorID) *genesisspectypes.SignedPartialSignatureMessage {
	ret := contributionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot, testingutils.TestingDutySlot, false, false)
	msg := &genesisspectypes.PartialSignatureMessages{
		Type:     genesisspectypes.ContributionProofs,
		Slot:     testingutils.TestingDutySlot,
		Messages: ret.Message.Messages[0:2],
	}

	msgSig, _ := testingutils.NewTestingKeyManager().SignRoot(msg, genesisspectypes.PartialSignatureType, beaconSK.GetPublicKey().Serialize())
	return &genesisspectypes.SignedPartialSignatureMessage{
		Message:   *msg,
		Signature: msgSig,
		Signer:    msgID,
	}
}

var contributionProofMsg = func(
	sk, beaconsk *bls.SecretKey,
	id, beaconid genesisspectypes.OperatorID,
	slot spec.Slot,
	msgSlot spec.Slot,
	wrongMsgOrder bool,
	wrongBeaconSig bool,
) *genesisspectypes.SignedPartialSignatureMessage {
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(1, genesisspectypes.DomainSyncCommitteeSelectionProof)

	msgs := make([]*genesisspectypes.PartialSignatureMessage, 0)
	for index := range testingutils.TestingContributionProofIndexes {
		subnet, _ := beacon.SyncCommitteeSubnetID(spec.CommitteeIndex(index))
		data := &altair.SyncAggregatorSelectionData{
			Slot:              slot,
			SubcommitteeIndex: subnet,
		}
		sig, root, _ := signer.SignBeaconObject(data, d, beaconsk.GetPublicKey().Serialize(), genesisspectypes.DomainSyncCommitteeSelectionProof)
		if wrongBeaconSig {
			sig, root, _ = signer.SignBeaconObject(data, d, testingutils.Testing7SharesSet().ValidatorPK.Serialize(), genesisspectypes.DomainSyncCommitteeSelectionProof)
		}

		msg := &genesisspectypes.PartialSignatureMessage{
			PartialSignature: sig[:],
			SigningRoot:      ensureRoot(root),
			Signer:           beaconid,
		}

		msgs = append(msgs, msg)
	}

	if wrongMsgOrder {
		m := msgs[0]
		msgs[0] = msgs[1]
		msgs[1] = m
	}

	msg := &genesisspectypes.PartialSignatureMessages{
		Type:     genesisspectypes.ContributionProofs,
		Slot:     testingutils.TestingDutySlot,
		Messages: msgs,
	}

	msgSig, _ := signer.SignRoot(msg, genesisspectypes.PartialSignatureType, sk.GetPublicKey().Serialize())
	return &genesisspectypes.SignedPartialSignatureMessage{
		Message:   *msg,
		Signature: msgSig,
		Signer:    id,
	}
}

// ensureRoot ensures that SigningRoot will have sufficient allocated memory
// otherwise we get panic from bls:
// github.com/herumi/bls-eth-go-binary/bls.(*Sign).VerifyByte:738
func ensureRoot(root [32]byte) [32]byte {
	tmp := [32]byte{}
	copy(tmp[:], root[:])
	return tmp
}
