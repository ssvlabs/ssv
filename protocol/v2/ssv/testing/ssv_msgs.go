package testing

import (
	"crypto/sha256"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
)

var TestingSSVDomainType = spectypes.JatoTestnet
var TestingForkData = spectypes.ForkData{Epoch: spectestingutils.TestingDutyEpoch, Domain: TestingSSVDomainType}
var CommitteeMsgID = func() []byte {
	ret := spectypes.NewMsgID(TestingSSVDomainType, spectestingutils.TestingValidatorPubKey[:], spectypes.RoleCommittee)
	return ret[:]
}()
var AttesterMsgID = func() []byte {
	ret := spectypes.NewMsgID(TestingSSVDomainType, spectestingutils.TestingValidatorPubKey[:], spectypes.RoleCommittee)
	return ret[:]
}()
var ProposerMsgID = func() []byte {
	ret := spectypes.NewMsgID(TestingSSVDomainType, spectestingutils.TestingValidatorPubKey[:], spectypes.RoleProposer)
	return ret[:]
}()
var AggregatorMsgID = func() []byte {
	ret := spectypes.NewMsgID(TestingSSVDomainType, spectestingutils.TestingValidatorPubKey[:], spectypes.RoleAggregator)
	return ret[:]
}()
var SyncCommitteeMsgID = func() []byte {
	ret := spectypes.NewMsgID(TestingSSVDomainType, spectestingutils.TestingValidatorPubKey[:], spectypes.RoleCommittee)
	return ret[:]
}()
var SyncCommitteeContributionMsgID = func() []byte {
	ret := spectypes.NewMsgID(TestingSSVDomainType, spectestingutils.TestingValidatorPubKey[:], spectypes.RoleSyncCommitteeContribution)
	return ret[:]
}()
var ValidatorRegistrationMsgID = func() []byte {
	ret := spectypes.NewMsgID(TestingSSVDomainType, spectestingutils.TestingValidatorPubKey[:], spectypes.RoleValidatorRegistration)
	return ret[:]
}()
var VoluntaryExitMsgID = func() []byte {
	ret := spectypes.NewMsgID(TestingSSVDomainType, spectestingutils.TestingValidatorPubKey[:], spectypes.RoleVoluntaryExit)
	return ret[:]
}()

var EncodeConsensusDataTest = func(cd *spectypes.ValidatorConsensusData) []byte {
	encodedCD, _ := cd.Encode()
	return encodedCD
}

// BeaconVote data - Committee Runner

var TestBeaconVoteByts, _ = spectestingutils.TestBeaconVote.Encode()

var TestBeaconVoteNextEpochByts, _ = spectestingutils.TestBeaconVoteNextEpoch.Encode()

// ConsensusData - Attester

var TestAttesterConsensusData = &spectypes.ValidatorConsensusData{
	Duty:    *spectestingutils.TestingAttesterDuty.ValidatorDuties[0],
	DataSSZ: spectestingutils.TestingAttestationDataBytes,
	Version: spec.DataVersionPhase0,
}
var TestAttesterConsensusDataByts, _ = TestAttesterConsensusData.Encode()

var TestAttesterNextEpochConsensusData = &spectypes.ValidatorConsensusData{
	Duty:    *spectestingutils.TestingAttesterDutyNextEpoch.ValidatorDuties[0],
	DataSSZ: spectestingutils.TestingAttestationNextEpochDataBytes,
	Version: spec.DataVersionPhase0,
}

var TestingAttesterNextEpochConsensusDataByts, _ = TestAttesterNextEpochConsensusData.Encode()

// ConsensusData - Aggregator

var TestAggregatorConsensusData = &spectypes.ValidatorConsensusData{
	Duty:    spectestingutils.TestingAggregatorDuty,
	DataSSZ: spectestingutils.TestingAggregateAndProofBytes,
	Version: spec.DataVersionPhase0,
}
var TestAggregatorConsensusDataByts, _ = TestAggregatorConsensusData.Encode()

var TestAttesterWithJustificationsConsensusData = func(ks *spectestingutils.TestKeySet) *spectypes.ValidatorConsensusData {
	justif := make([]*spectypes.PartialSignatureMessages, 0)
	for i := uint64(1); i <= ks.Threshold; i++ {
		justif = append(justif, PreConsensusRandaoMsg(ks.Shares[i], i))
	}

	return &spectypes.ValidatorConsensusData{
		Duty:                       *spectestingutils.TestingAttesterDuty.ValidatorDuties[0],
		Version:                    spec.DataVersionDeneb,
		PreConsensusJustifications: justif,
		DataSSZ:                    spectestingutils.TestingAttestationDataBytes,
	}
}

var TestAggregatorWithJustificationsConsensusData = func(ks *spectestingutils.TestKeySet) *spectypes.ValidatorConsensusData {
	justif := make([]*spectypes.PartialSignatureMessages, 0)
	for i := uint64(1); i <= ks.Threshold; i++ {
		justif = append(justif, PreConsensusSelectionProofMsg(ks.Shares[i], ks.Shares[i], i, i))
	}

	return &spectypes.ValidatorConsensusData{
		Duty:                       spectestingutils.TestingAggregatorDuty,
		Version:                    spec.DataVersionBellatrix,
		PreConsensusJustifications: justif,
		DataSSZ:                    spectestingutils.TestingAggregateAndProofBytes,
	}

}

// ConsensusData - Sync Committee

// TestSyncCommitteeWithJustificationsConsensusData is an invalid sync committee msg (doesn't have pre-consensus)
var TestSyncCommitteeWithJustificationsConsensusData = func(ks *spectestingutils.TestKeySet) *spectypes.ValidatorConsensusData {
	justif := make([]*spectypes.PartialSignatureMessages, 0)
	for i := uint64(0); i <= ks.Threshold; i++ {
		justif = append(justif, PreConsensusRandaoMsg(ks.Shares[i+1], i+1))
	}

	return &spectypes.ValidatorConsensusData{
		Duty:                       *spectestingutils.TestingSyncCommitteeDuty.ValidatorDuties[0],
		Version:                    spec.DataVersionDeneb,
		PreConsensusJustifications: justif,
		DataSSZ:                    spectestingutils.TestingSyncCommitteeBlockRoot[:],
	}
}

var TestSyncCommitteeConsensusData = &spectypes.ValidatorConsensusData{
	Duty:    *spectestingutils.TestingSyncCommitteeDuty.ValidatorDuties[0],
	DataSSZ: spectestingutils.TestingSyncCommitteeBlockRoot[:],
	Version: spec.DataVersionPhase0,
}
var TestSyncCommitteeConsensusDataByts, _ = TestSyncCommitteeConsensusData.Encode()

var TestSyncCommitteeNextEpochConsensusData = &spectypes.ValidatorConsensusData{
	Duty:    *spectestingutils.TestingSyncCommitteeDutyNextEpoch.ValidatorDuties[0],
	DataSSZ: spectestingutils.TestingSyncCommitteeBlockRoot[:],
	Version: spec.DataVersionPhase0,
}

var TestSyncCommitteeNextEpochConsensusDataByts, _ = TestSyncCommitteeNextEpochConsensusData.Encode()

// ConsensusData - Sync Committee Contribution

var TestSyncCommitteeContributionConsensusData = &spectypes.ValidatorConsensusData{
	Duty:    spectestingutils.TestingSyncCommitteeContributionDuty,
	DataSSZ: spectestingutils.TestingContributionsDataBytes,
	Version: spec.DataVersionPhase0,
}
var TestSyncCommitteeContributionConsensusDataByts, _ = TestSyncCommitteeContributionConsensusData.Encode()
var TestSyncCommitteeContributionConsensusDataRoot = func() [32]byte {
	return sha256.Sum256(TestSyncCommitteeContributionConsensusDataByts)
}()

var TestConsensusUnkownDutyTypeData = &spectypes.ValidatorConsensusData{
	Duty:    spectestingutils.TestingUnknownDutyType,
	DataSSZ: spectestingutils.TestingAttestationDataBytes,
	Version: spec.DataVersionPhase0,
}
var TestConsensusUnkownDutyTypeDataByts, _ = TestConsensusUnkownDutyTypeData.Encode()

var TestConsensusWrongDutyPKData = &spectypes.ValidatorConsensusData{
	Duty:    spectestingutils.TestingWrongDutyPK,
	DataSSZ: spectestingutils.TestingAttestationDataBytes,
	Version: spec.DataVersionPhase0,
}
var TestConsensusWrongDutyPKDataByts, _ = TestConsensusWrongDutyPKData.Encode()

var SSVMsgAttester = func(qbftMsg *spectypes.SignedSSVMessage, partialSigMsg *spectypes.PartialSignatureMessages) *spectypes.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, spectypes.NewMsgID(TestingSSVDomainType, spectestingutils.TestingValidatorPubKey[:], spectypes.RoleCommittee))
}

var SSVMsgWrongID = func(qbftMsg *spectypes.SignedSSVMessage, partialSigMsg *spectypes.PartialSignatureMessages) *spectypes.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, spectypes.NewMsgID(TestingSSVDomainType, spectestingutils.TestingWrongValidatorPubKey[:], spectypes.RoleCommittee))
}

var SSVMsgCommittee = func(qbftMsg *spectypes.SignedSSVMessage, partialSigMsg *spectypes.PartialSignatureMessages) *spectypes.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, spectypes.NewMsgID(TestingSSVDomainType, spectestingutils.TestingValidatorPubKey[:], spectypes.RoleCommittee))
}

var SSVMsgProposer = func(qbftMsg *spectypes.SignedSSVMessage, partialSigMsg *spectypes.PartialSignatureMessages) *spectypes.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, spectypes.NewMsgID(TestingSSVDomainType, spectestingutils.TestingValidatorPubKey[:], spectypes.RoleProposer))
}

var SSVMsgAggregator = func(qbftMsg *spectypes.SignedSSVMessage, partialSigMsg *spectypes.PartialSignatureMessages) *spectypes.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, spectypes.NewMsgID(TestingSSVDomainType, spectestingutils.TestingValidatorPubKey[:], spectypes.RoleAggregator))
}

var SSVMsgSyncCommittee = func(qbftMsg *spectypes.SignedSSVMessage, partialSigMsg *spectypes.PartialSignatureMessages) *spectypes.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, spectypes.NewMsgID(TestingSSVDomainType, spectestingutils.TestingValidatorPubKey[:], spectypes.RoleCommittee))
}

var SSVMsgSyncCommitteeContribution = func(qbftMsg *spectypes.SignedSSVMessage, partialSigMsg *spectypes.PartialSignatureMessages) *spectypes.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, spectypes.NewMsgID(TestingSSVDomainType, spectestingutils.TestingValidatorPubKey[:], spectypes.RoleSyncCommitteeContribution))
}

var SSVMsgValidatorRegistration = func(qbftMsg *spectypes.SignedSSVMessage, partialSigMsg *spectypes.PartialSignatureMessages) *spectypes.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, spectypes.NewMsgID(TestingSSVDomainType, spectestingutils.TestingValidatorPubKey[:], spectypes.RoleValidatorRegistration))
}

var SSVMsgVoluntaryExit = func(qbftMsg *spectypes.SignedSSVMessage, partialSigMsg *spectypes.PartialSignatureMessages) *spectypes.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, spectypes.NewMsgID(TestingSSVDomainType, spectestingutils.TestingValidatorPubKey[:], spectypes.RoleVoluntaryExit))
}

var ssvMsg = func(qbftMsg *spectypes.SignedSSVMessage, postMsg *spectypes.PartialSignatureMessages, msgID spectypes.MessageID) *spectypes.SSVMessage {

	if qbftMsg != nil {
		return &spectypes.SSVMessage{
			MsgType: qbftMsg.SSVMessage.MsgType,
			MsgID:   msgID,
			Data:    qbftMsg.SSVMessage.Data,
		}
	}

	if postMsg != nil {
		msgType := spectypes.SSVPartialSignatureMsgType
		data, err := postMsg.Encode()
		if err != nil {
			panic(err)
		}
		return &spectypes.SSVMessage{
			MsgType: msgType,
			MsgID:   msgID,
			Data:    data,
		}
	}

	panic("msg type undefined")
}

var PostConsensusWrongAttestationMsg = func(sk *bls.SecretKey, id spectypes.OperatorID, height specqbft.Height) *spectypes.PartialSignatureMessages {
	return postConsensusAttestationMsg(sk, id, height, true, false)
}

var PostConsensusWrongValidatorIndexAttestationMsg = func(sk *bls.SecretKey, id spectypes.OperatorID, height specqbft.Height) *spectypes.PartialSignatureMessages {
	msg := postConsensusAttestationMsg(sk, id, height, true, false)
	for _, m := range msg.Messages {
		m.ValidatorIndex = spectestingutils.TestingWrongValidatorIndex
	}
	return msg
}

var PostConsensusWrongSigAttestationMsg = func(sk *bls.SecretKey, id spectypes.OperatorID, height specqbft.Height) *spectypes.PartialSignatureMessages {
	return postConsensusAttestationMsg(sk, id, height, false, true)
}

var PostConsensusAttestationMsg = func(sk *bls.SecretKey, id spectypes.OperatorID, height specqbft.Height) *spectypes.PartialSignatureMessages {
	return postConsensusAttestationMsg(sk, id, height, false, false)
}

var PostConsensusAttestationTooManyRootsMsg = func(sk *bls.SecretKey, id spectypes.OperatorID, height specqbft.Height) *spectypes.PartialSignatureMessages {
	ret := postConsensusAttestationMsg(sk, id, height, false, false)
	ret.Messages = append(ret.Messages, ret.Messages[0])

	msg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     spectestingutils.TestingDutySlot,
		Messages: ret.Messages,
	}
	return msg
}

var PostConsensusAttestationTooFewRootsMsg = func(sk *bls.SecretKey, id spectypes.OperatorID, height specqbft.Height) *spectypes.PartialSignatureMessages {
	msg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     spectestingutils.TestingDutySlot,
		Messages: []*spectypes.PartialSignatureMessage{},
	}
	return msg
}

var postConsensusAttestationMsg = func(
	sk *bls.SecretKey,
	id spectypes.OperatorID,
	height specqbft.Height,
	wrongRoot bool,
	wrongBeaconSig bool,
) *spectypes.PartialSignatureMessages {
	signer := spectestingutils.NewTestingKeyManager()
	beacon := spectestingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(spectestingutils.TestingAttestationData.Target.Epoch, spectypes.DomainAttester)

	attData := spectestingutils.TestingAttestationData
	if wrongRoot {
		attData = spectestingutils.TestingWrongAttestationData
	}

	signed, root, _ := signer.SignBeaconObject(attData, d, sk.GetPublicKey().Serialize(), spectypes.DomainAttester)

	if wrongBeaconSig {
		signed, _, _ = signer.SignBeaconObject(attData, d, spectestingutils.Testing7SharesSet().ValidatorPK.Serialize(), spectypes.DomainAttester)
	}

	msgs := spectypes.PartialSignatureMessages{
		Type: spectypes.PostConsensusPartialSig,
		Slot: spectestingutils.TestingDutySlot,
		Messages: []*spectypes.PartialSignatureMessage{
			{
				PartialSignature: signed,
				SigningRoot:      root,
				Signer:           id,
				ValidatorIndex:   spectestingutils.TestingValidatorIndex,
			},
		},
	}
	return &msgs
}

// Post Consensus - Attestation and Sync Committee

var PostConsensusAttestationAndSyncCommitteeMsg = func(sk *bls.SecretKey, id spectypes.OperatorID, height specqbft.Height) *spectypes.PartialSignatureMessages {
	return postConsensusAttestationAndSyncCommitteeMsg(sk, id, height, false, false)
}

var PostConsensusWrongAttestationAndSyncCommitteeMsg = func(sk *bls.SecretKey, id spectypes.OperatorID, height specqbft.Height) *spectypes.PartialSignatureMessages {
	return postConsensusAttestationAndSyncCommitteeMsg(sk, id, height, true, false)
}

var PostConsensusWrongValidatorIndexAttestationAndSyncCommitteeMsg = func(sk *bls.SecretKey, id spectypes.OperatorID, height specqbft.Height) *spectypes.PartialSignatureMessages {
	msg := postConsensusAttestationAndSyncCommitteeMsg(sk, id, height, true, false)
	for _, m := range msg.Messages {
		m.ValidatorIndex = spectestingutils.TestingWrongValidatorIndex
	}
	return msg
}

var PostConsensusWrongSigAttestationAndSyncCommitteeMsg = func(sk *bls.SecretKey, id spectypes.OperatorID, height specqbft.Height) *spectypes.PartialSignatureMessages {
	return postConsensusAttestationAndSyncCommitteeMsg(sk, id, height, false, true)
}

var PostConsensusAttestationAndSyncCommitteeMsgTooManyRootsMsg = func(sk *bls.SecretKey, id spectypes.OperatorID, height specqbft.Height) *spectypes.PartialSignatureMessages {
	ret := postConsensusAttestationAndSyncCommitteeMsg(sk, id, height, false, false)
	ret.Messages = append(ret.Messages, ret.Messages[0])

	msg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     spectestingutils.TestingDutySlot,
		Messages: ret.Messages,
	}
	return msg
}

var PostConsensusAttestationAndSyncCommitteeMsgTooFewRootsMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	msg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     spectestingutils.TestingDutySlot,
		Messages: []*spectypes.PartialSignatureMessage{},
	}
	return msg
}

var postConsensusAttestationAndSyncCommitteeMsg = func(
	sk *bls.SecretKey,
	id spectypes.OperatorID,
	height specqbft.Height,
	wrongRoot bool,
	wrongBeaconSig bool,
) *spectypes.PartialSignatureMessages {
	attestationPSigMsg := postConsensusAttestationMsg(sk, id, height, wrongRoot, wrongBeaconSig)
	syncCommitteePSigMessage := postConsensusSyncCommitteeMsg(sk, id, wrongRoot, wrongBeaconSig)

	attestationPSigMsg.Messages = append(attestationPSigMsg.Messages, syncCommitteePSigMessage.Messages...)

	return attestationPSigMsg
}

var PreConsensusFailedMsg = func(msgSigner *bls.SecretKey, msgSignerID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	signer := spectestingutils.NewTestingKeyManager()
	beacon := spectestingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(spectestingutils.TestingDutyEpoch, spectypes.DomainRandao)
	signed, root, _ := signer.SignBeaconObject(spectypes.SSZUint64(spectestingutils.TestingDutyEpoch), d, msgSigner.GetPublicKey().Serialize(), spectypes.DomainRandao)

	msg := spectypes.PartialSignatureMessages{
		Type: spectypes.RandaoPartialSig,
		Slot: spectestingutils.TestingDutySlot,
		Messages: []*spectypes.PartialSignatureMessage{
			{
				PartialSignature: signed[:],
				SigningRoot:      root,
				Signer:           msgSignerID,
				ValidatorIndex:   spectestingutils.TestingValidatorIndex,
			},
		},
	}
	return &msg
}

var PreConsensusRandaoMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return randaoMsg(sk, id, false, spectestingutils.TestingDutyEpoch, 1, false)
}

var randaoMsg = func(
	sk *bls.SecretKey,
	id spectypes.OperatorID,
	wrongRoot bool,
	epoch phase0.Epoch,
	msgCnt int,
	wrongBeaconSig bool,
) *spectypes.PartialSignatureMessages {
	signer := spectestingutils.NewTestingKeyManager()
	beacon := spectestingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(epoch, spectypes.DomainRandao)
	signed, root, _ := signer.SignBeaconObject(spectypes.SSZUint64(epoch), d, sk.GetPublicKey().Serialize(), spectypes.DomainRandao)
	if wrongBeaconSig {
		signed, root, _ = signer.SignBeaconObject(spectypes.SSZUint64(spectestingutils.TestingDutyEpoch), d, spectestingutils.Testing7SharesSet().ValidatorPK.Serialize(), spectypes.DomainRandao)
	}

	msgs := spectypes.PartialSignatureMessages{
		Type:     spectypes.RandaoPartialSig,
		Slot:     spectestingutils.TestingDutySlot,
		Messages: []*spectypes.PartialSignatureMessage{},
	}
	for i := 0; i < msgCnt; i++ {
		msg := &spectypes.PartialSignatureMessage{
			PartialSignature: signed[:],
			SigningRoot:      root,
			Signer:           id,
			ValidatorIndex:   spectestingutils.TestingValidatorIndex,
		}
		if wrongRoot {
			msg.SigningRoot = [32]byte{}
		}
		msgs.Messages = append(msgs.Messages, msg)
	}

	return &msgs
}

var PreConsensusSelectionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return PreConsensusCustomSlotSelectionProofMsg(msgSK, beaconSK, msgID, beaconID, spectestingutils.TestingDutySlot)
}

var PreConsensusSelectionProofWrongBeaconSigMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return selectionProofMsg(msgSK, beaconSK, msgID, beaconID, spectestingutils.TestingDutySlot, spectestingutils.TestingDutySlot, 1, true)
}

var PreConsensusSelectionProofNextEpochMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return selectionProofMsg(msgSK, beaconSK, msgID, beaconID, spectestingutils.TestingDutySlot2, spectestingutils.TestingDutySlot2, 1, false)
}

var PreConsensusSelectionProofTooManyRootsMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return selectionProofMsg(msgSK, beaconSK, msgID, beaconID, spectestingutils.TestingDutySlot, spectestingutils.TestingDutySlot, 3, false)
}

var PreConsensusSelectionProofTooFewRootsMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return selectionProofMsg(msgSK, beaconSK, msgID, beaconID, spectestingutils.TestingDutySlot, spectestingutils.TestingDutySlot, 0, false)
}

var PreConsensusCustomSlotSelectionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID, slot phase0.Slot) *spectypes.PartialSignatureMessages {
	return selectionProofMsg(msgSK, beaconSK, msgID, beaconID, slot, spectestingutils.TestingDutySlot, 1, false)
}

var PreConsensusWrongMsgSlotSelectionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return selectionProofMsg(msgSK, beaconSK, msgID, beaconID, spectestingutils.TestingDutySlot, spectestingutils.TestingDutySlot+1, 1, false)
}

var TestSelectionProofWithJustificationsConsensusData = func(ks *spectestingutils.TestKeySet) *spectypes.ValidatorConsensusData {
	justif := make([]*spectypes.PartialSignatureMessages, 0)
	for i := uint64(0); i <= ks.Threshold; i++ {
		justif = append(justif, PreConsensusSelectionProofMsg(ks.Shares[i+1], ks.Shares[i+1], i+1, i+1))
	}

	return &spectypes.ValidatorConsensusData{
		Duty:                       spectestingutils.TestingAggregatorDuty,
		Version:                    spec.DataVersionDeneb,
		PreConsensusJustifications: justif,
		DataSSZ:                    spectestingutils.TestingAggregateAndProofBytes,
	}
}

var selectionProofMsg = func(
	sk *bls.SecretKey,
	beaconsk *bls.SecretKey,
	id spectypes.OperatorID,
	beaconid spectypes.OperatorID,
	slot phase0.Slot,
	msgSlot phase0.Slot,
	msgCnt int,
	wrongBeaconSig bool,
) *spectypes.PartialSignatureMessages {
	signer := spectestingutils.NewTestingKeyManager()
	beacon := spectestingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(1, spectypes.DomainSelectionProof)
	signed, root, _ := signer.SignBeaconObject(spectypes.SSZUint64(slot), d, beaconsk.GetPublicKey().Serialize(), spectypes.DomainSelectionProof)
	if wrongBeaconSig {
		signed, root, _ = signer.SignBeaconObject(spectypes.SSZUint64(slot), d, spectestingutils.Testing7SharesSet().ValidatorPK.Serialize(), spectypes.DomainSelectionProof)
	}

	_msgs := make([]*spectypes.PartialSignatureMessage, 0)
	for i := 0; i < msgCnt; i++ {
		_msgs = append(_msgs, &spectypes.PartialSignatureMessage{
			PartialSignature: signed[:],
			SigningRoot:      root,
			Signer:           beaconid,
			ValidatorIndex:   spectestingutils.TestingValidatorIndex,
		})
	}

	msgs := spectypes.PartialSignatureMessages{
		Type:     spectypes.SelectionProofPartialSig,
		Slot:     spectestingutils.TestingDutySlot,
		Messages: _msgs,
	}
	return &msgs
}

var PreConsensusValidatorRegistrationMsg = func(msgSK *bls.SecretKey, msgID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return validatorRegistrationMsg(msgSK, msgSK, msgID, msgID, 1, false, spectestingutils.TestingDutySlot, false)
}

var PreConsensusValidatorRegistrationTooFewRootsMsg = func(msgSK *bls.SecretKey, msgID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return validatorRegistrationMsg(msgSK, msgSK, msgID, msgID, 0, false, spectestingutils.TestingDutySlot, false)
}

var PreConsensusValidatorRegistrationTooManyRootsMsg = func(msgSK *bls.SecretKey, msgID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return validatorRegistrationMsg(msgSK, msgSK, msgID, msgID, 2, false, spectestingutils.TestingDutySlot, false)
}

var PreConsensusValidatorRegistrationWrongBeaconSigMsg = func(msgSK *bls.SecretKey, msgID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return validatorRegistrationMsg(msgSK, msgSK, msgID, msgID, 1, false, spectestingutils.TestingDutySlot, true)
}

var PreConsensusValidatorRegistrationWrongRootMsg = func(msgSK *bls.SecretKey, msgID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return validatorRegistrationMsg(msgSK, msgSK, msgID, msgID, 1, true, spectestingutils.TestingDutySlot, false)
}

var PreConsensusValidatorRegistrationNextEpochMsg = func(msgSK *bls.SecretKey, msgID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return validatorRegistrationMsg(msgSK, msgSK, msgID, msgID, 1, false, spectestingutils.TestingDutySlot2, false)
}

var validatorRegistrationMsg = func(
	sk, beaconSK *bls.SecretKey,
	id, beaconID spectypes.OperatorID,
	msgCnt int,
	wrongRoot bool,
	slot phase0.Slot,
	wrongBeaconSig bool,
) *spectypes.PartialSignatureMessages {
	signer := spectestingutils.NewTestingKeyManager()
	beacon := spectestingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(spectestingutils.TestingDutyEpoch, spectypes.DomainApplicationBuilder)

	signed, root, _ := signer.SignBeaconObject(spectestingutils.TestingValidatorRegistrationBySlot(slot), d,
		beaconSK.GetPublicKey().Serialize(),
		spectypes.DomainApplicationBuilder)
	if wrongRoot {
		signed, root, _ = signer.SignBeaconObject(spectestingutils.TestingValidatorRegistrationWrong, d, beaconSK.GetPublicKey().Serialize(), spectypes.DomainApplicationBuilder)
	}
	if wrongBeaconSig {
		signed, root, _ = signer.SignBeaconObject(spectestingutils.TestingValidatorRegistration, d, spectestingutils.Testing7SharesSet().ValidatorPK.Serialize(), spectypes.DomainApplicationBuilder)
	}

	msgs := spectypes.PartialSignatureMessages{
		Type:     spectypes.ValidatorRegistrationPartialSig,
		Slot:     slot,
		Messages: []*spectypes.PartialSignatureMessage{},
	}

	for i := 0; i < msgCnt; i++ {
		msg := &spectypes.PartialSignatureMessage{
			PartialSignature: signed[:],
			SigningRoot:      root,
			Signer:           beaconID,
			ValidatorIndex:   spectestingutils.TestingValidatorIndex,
		}
		msgs.Messages = append(msgs.Messages, msg)
	}

	msg := &spectypes.PartialSignatureMessage{
		PartialSignature: signed[:],
		SigningRoot:      root,
		Signer:           id,
		ValidatorIndex:   spectestingutils.TestingValidatorIndex,
	}
	if wrongRoot {
		msg.SigningRoot = [32]byte{}
	}

	return &msgs
}

var PreConsensusVoluntaryExitMsg = func(msgSK *bls.SecretKey, msgID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return VoluntaryExitMsg(msgSK, msgSK, msgID, msgID, 1, false, spectestingutils.TestingDutySlot, false)
}

var PreConsensusVoluntaryExitNextEpochMsg = func(msgSK *bls.SecretKey, msgID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return VoluntaryExitMsg(msgSK, msgSK, msgID, msgID, 1, false, spectestingutils.TestingDutySlot2, false)
}

var PreConsensusVoluntaryExitTooFewRootsMsg = func(msgSK *bls.SecretKey, msgID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return VoluntaryExitMsg(msgSK, msgSK, msgID, msgID, 0, false, spectestingutils.TestingDutySlot, false)
}

var PreConsensusVoluntaryExitTooManyRootsMsg = func(msgSK *bls.SecretKey, msgID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return VoluntaryExitMsg(msgSK, msgSK, msgID, msgID, 2, false, spectestingutils.TestingDutySlot, false)
}

var PreConsensusVoluntaryExitWrongBeaconSigMsg = func(msgSK *bls.SecretKey, msgID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return VoluntaryExitMsg(msgSK, msgSK, msgID, msgID, 1, false, spectestingutils.TestingDutySlot, true)
}

var PreConsensusVoluntaryExitWrongRootMsg = func(msgSK *bls.SecretKey, msgID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return VoluntaryExitMsg(msgSK, msgSK, msgID, msgID, 1, true, spectestingutils.TestingDutySlot, false)
}

var VoluntaryExitMsg = func(
	sk, beaconSK *bls.SecretKey,
	id, beaconID spectypes.OperatorID,
	msgCnt int,
	wrongRoot bool,
	slot phase0.Slot,
	wrongBeaconSig bool,
) *spectypes.PartialSignatureMessages {
	signer := spectestingutils.NewTestingKeyManager()
	beacon := spectestingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(spectestingutils.TestingDutyEpoch, spectypes.DomainVoluntaryExit)

	signed, root, _ := signer.SignBeaconObject(spectestingutils.TestingVoluntaryExitBySlot(slot), d,
		beaconSK.GetPublicKey().Serialize(),
		spectypes.DomainVoluntaryExit)
	if wrongRoot {
		signed, root, _ = signer.SignBeaconObject(spectestingutils.TestingVoluntaryExitWrong, d, beaconSK.GetPublicKey().Serialize(), spectypes.DomainVoluntaryExit)
	}
	if wrongBeaconSig {
		signed, root, _ = signer.SignBeaconObject(spectestingutils.TestingVoluntaryExit, d, spectestingutils.Testing7SharesSet().ValidatorPK.Serialize(), spectypes.DomainVoluntaryExit)
	}

	msgs := spectypes.PartialSignatureMessages{
		Type:     spectypes.VoluntaryExitPartialSig,
		Slot:     slot,
		Messages: []*spectypes.PartialSignatureMessage{},
	}

	for i := 0; i < msgCnt; i++ {
		msg := &spectypes.PartialSignatureMessage{
			PartialSignature: signed[:],
			SigningRoot:      root,
			Signer:           beaconID,
			ValidatorIndex:   spectestingutils.TestingValidatorIndex,
		}
		msgs.Messages = append(msgs.Messages, msg)
	}

	msg := &spectypes.PartialSignatureMessage{
		PartialSignature: signed[:],
		SigningRoot:      root,
		Signer:           id,
		ValidatorIndex:   spectestingutils.TestingValidatorIndex,
	}
	if wrongRoot {
		msg.SigningRoot = [32]byte{}
	}

	return &msgs
}

var PostConsensusAggregatorMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return postConsensusAggregatorMsg(sk, id, false, false)
}

var PostConsensusAggregatorTooManyRootsMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	ret := postConsensusAggregatorMsg(sk, id, false, false)
	ret.Messages = append(ret.Messages, ret.Messages[0])

	msg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     spectestingutils.TestingDutySlot,
		Messages: ret.Messages,
	}
	return msg
}

var PostConsensusAggregatorTooFewRootsMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	msg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     spectestingutils.TestingDutySlot,
		Messages: []*spectypes.PartialSignatureMessage{},
	}
	return msg
}

var PostConsensusWrongAggregatorMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return postConsensusAggregatorMsg(sk, id, true, false)
}

var PostConsensusWrongValidatorIndexAggregatorMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	msg := postConsensusAggregatorMsg(sk, id, true, false)
	for _, m := range msg.Messages {
		m.ValidatorIndex = spectestingutils.TestingWrongValidatorIndex
	}
	return msg
}

var PostConsensusWrongSigAggregatorMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return postConsensusAggregatorMsg(sk, id, false, true)
}

var postConsensusAggregatorMsg = func(
	sk *bls.SecretKey,
	id spectypes.OperatorID,
	wrongRoot bool,
	wrongBeaconSig bool,
) *spectypes.PartialSignatureMessages {
	signer := spectestingutils.NewTestingKeyManager()
	beacon := spectestingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(1, spectypes.DomainAggregateAndProof)

	aggData := spectestingutils.TestingAggregateAndProof
	if wrongRoot {
		aggData = spectestingutils.TestingWrongAggregateAndProof
	}

	signed, root, _ := signer.SignBeaconObject(aggData, d, sk.GetPublicKey().Serialize(), spectypes.DomainAggregateAndProof)
	if wrongBeaconSig {
		signed, root, _ = signer.SignBeaconObject(aggData, d, spectestingutils.Testing7SharesSet().ValidatorPK.Serialize(), spectypes.DomainAggregateAndProof)
	}

	msgs := spectypes.PartialSignatureMessages{
		Type: spectypes.PostConsensusPartialSig,
		Slot: spectestingutils.TestingDutySlot,
		Messages: []*spectypes.PartialSignatureMessage{
			{
				PartialSignature: signed,
				SigningRoot:      root,
				Signer:           id,
				ValidatorIndex:   spectestingutils.TestingValidatorIndex,
			},
		},
	}
	return &msgs
}

var PostConsensusSyncCommitteeMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return postConsensusSyncCommitteeMsg(sk, id, false, false)
}

var PostConsensusSyncCommitteeTooManyRootsMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	ret := postConsensusSyncCommitteeMsg(sk, id, false, false)
	ret.Messages = append(ret.Messages, ret.Messages[0])

	msg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     spectestingutils.TestingDutySlot,
		Messages: ret.Messages,
	}
	return msg
}

var PostConsensusSyncCommitteeTooFewRootsMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	msg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     spectestingutils.TestingDutySlot,
		Messages: []*spectypes.PartialSignatureMessage{},
	}
	return msg
}

var PostConsensusWrongSyncCommitteeMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return postConsensusSyncCommitteeMsg(sk, id, true, false)
}

var PostConsensusWrongValidatorIndexSyncCommitteeMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	msg := postConsensusSyncCommitteeMsg(sk, id, true, false)
	for _, m := range msg.Messages {
		m.ValidatorIndex = spectestingutils.TestingWrongValidatorIndex
	}
	return msg
}

var PostConsensusWrongSigSyncCommitteeMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return postConsensusSyncCommitteeMsg(sk, id, false, true)
}

var postConsensusSyncCommitteeMsg = func(
	sk *bls.SecretKey,
	id spectypes.OperatorID,
	wrongRoot bool,
	wrongBeaconSig bool,
) *spectypes.PartialSignatureMessages {
	signer := spectestingutils.NewTestingKeyManager()
	beacon := spectestingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(1, spectypes.DomainSyncCommittee)
	blockRoot := spectestingutils.TestingSyncCommitteeBlockRoot
	if wrongRoot {
		blockRoot = spectestingutils.TestingSyncCommitteeWrongBlockRoot
	}
	signed, root, _ := signer.SignBeaconObject(spectypes.SSZBytes(blockRoot[:]), d, sk.GetPublicKey().Serialize(), spectypes.DomainSyncCommittee)
	if wrongBeaconSig {
		signed, root, _ = signer.SignBeaconObject(spectypes.SSZBytes(blockRoot[:]), d, spectestingutils.Testing7SharesSet().ValidatorPK.Serialize(), spectypes.DomainSyncCommittee)
	}

	msgs := spectypes.PartialSignatureMessages{
		Type: spectypes.PostConsensusPartialSig,
		Slot: spectestingutils.TestingDutySlot,
		Messages: []*spectypes.PartialSignatureMessage{
			{
				PartialSignature: signed,
				SigningRoot:      root,
				Signer:           id,
				ValidatorIndex:   spectestingutils.TestingValidatorIndex,
			},
		},
	}
	return &msgs
}

var PreConsensusContributionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return PreConsensusCustomSlotContributionProofMsg(msgSK, beaconSK, msgID, beaconID, spectestingutils.TestingDutySlot)
}

var PreConsensusContributionProofWrongBeaconSigMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return contributionProofMsg(msgSK, beaconSK, msgID, beaconID, spectestingutils.TestingDutySlot, spectestingutils.TestingDutySlot+1, false, true)
}

var PreConsensusContributionProofNextEpochMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return contributionProofMsg(msgSK, beaconSK, msgID, beaconID, spectestingutils.TestingDutySlot2, spectestingutils.TestingDutySlot2, false, false)
}

var PreConsensusCustomSlotContributionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID, slot phase0.Slot) *spectypes.PartialSignatureMessages {
	return contributionProofMsg(msgSK, beaconSK, msgID, beaconID, slot, spectestingutils.TestingDutySlot, false, false)
}

var PreConsensusWrongMsgSlotContributionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return contributionProofMsg(msgSK, beaconSK, msgID, beaconID, spectestingutils.TestingDutySlot, spectestingutils.TestingDutySlot+1, false, false)
}

var PreConsensusWrongOrderContributionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return contributionProofMsg(msgSK, beaconSK, msgID, beaconID, spectestingutils.TestingDutySlot, spectestingutils.TestingDutySlot, true, false)
}
var TestContributionProofWithJustificationsConsensusData = func(ks *spectestingutils.TestKeySet) *spectypes.ValidatorConsensusData {
	justif := make([]*spectypes.PartialSignatureMessages, 0)
	for i := uint64(0); i <= ks.Threshold; i++ {
		justif = append(justif, PreConsensusContributionProofMsg(ks.Shares[i+1], ks.Shares[i+1], i+1, i+1))
	}

	return &spectypes.ValidatorConsensusData{
		Duty:                       spectestingutils.TestingSyncCommitteeContributionDuty,
		Version:                    spec.DataVersionDeneb,
		PreConsensusJustifications: justif,
		DataSSZ:                    spectestingutils.TestingContributionsDataBytes,
	}
}

var PreConsensusContributionProofTooManyRootsMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	ret := contributionProofMsg(msgSK, beaconSK, msgID, beaconID, spectestingutils.TestingDutySlot, spectestingutils.TestingDutySlot, false, false)
	msg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.ContributionProofs,
		Slot:     spectestingutils.TestingDutySlot,
		Messages: append(ret.Messages, ret.Messages[0]),
	}
	return msg
}

var PreConsensusContributionProofTooFewRootsMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	ret := contributionProofMsg(msgSK, beaconSK, msgID, beaconID, spectestingutils.TestingDutySlot, spectestingutils.TestingDutySlot, false, false)
	msg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.ContributionProofs,
		Slot:     spectestingutils.TestingDutySlot,
		Messages: ret.Messages[0:2],
	}
	return msg
}

var contributionProofMsg = func(
	sk, beaconsk *bls.SecretKey,
	id, beaconid spectypes.OperatorID,
	slot phase0.Slot,
	msgSlot phase0.Slot,
	wrongMsgOrder bool,
	wrongBeaconSig bool,
) *spectypes.PartialSignatureMessages {
	signer := spectestingutils.NewTestingKeyManager()
	beacon := spectestingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(1, spectypes.DomainSyncCommitteeSelectionProof)

	msgs := make([]*spectypes.PartialSignatureMessage, 0)
	for index := range spectestingutils.TestingContributionProofIndexes {
		subnet, _ := beacon.SyncCommitteeSubnetID(phase0.CommitteeIndex(index))
		data := &altair.SyncAggregatorSelectionData{
			Slot:              slot,
			SubcommitteeIndex: subnet,
		}
		sig, root, _ := signer.SignBeaconObject(data, d, beaconsk.GetPublicKey().Serialize(), spectypes.DomainSyncCommitteeSelectionProof)
		if wrongBeaconSig {
			sig, root, _ = signer.SignBeaconObject(data, d, spectestingutils.Testing7SharesSet().ValidatorPK.Serialize(), spectypes.DomainSyncCommitteeSelectionProof)
		}

		msg := &spectypes.PartialSignatureMessage{
			PartialSignature: sig[:],
			SigningRoot:      ensureRoot(root),
			Signer:           beaconid,
			ValidatorIndex:   spectestingutils.TestingValidatorIndex,
		}

		msgs = append(msgs, msg)
	}

	if wrongMsgOrder {
		m := msgs[0]
		msgs[0] = msgs[1]
		msgs[1] = m
	}

	msg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.ContributionProofs,
		Slot:     spectestingutils.TestingDutySlot,
		Messages: msgs,
	}
	return msg
}

var PostConsensusSyncCommitteeContributionMsg = func(sk *bls.SecretKey, id spectypes.OperatorID, keySet *spectestingutils.TestKeySet) *spectypes.PartialSignatureMessages {
	return postConsensusSyncCommitteeContributionMsg(sk, id, spectestingutils.TestingValidatorIndex, keySet, false, false, false)
}

var PostConsensusSyncCommitteeContributionWrongOrderMsg = func(sk *bls.SecretKey, id spectypes.OperatorID, keySet *spectestingutils.TestKeySet) *spectypes.PartialSignatureMessages {
	return postConsensusSyncCommitteeContributionMsg(sk, id, spectestingutils.TestingValidatorIndex, keySet, false, false, true)
}

var PostConsensusSyncCommitteeContributionTooManyRootsMsg = func(sk *bls.SecretKey, id spectypes.OperatorID, keySet *spectestingutils.TestKeySet) *spectypes.PartialSignatureMessages {
	ret := postConsensusSyncCommitteeContributionMsg(sk, id, spectestingutils.TestingValidatorIndex, keySet, false, false, false)
	ret.Messages = append(ret.Messages, ret.Messages[0])

	msg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     spectestingutils.TestingDutySlot,
		Messages: ret.Messages,
	}
	return msg
}

var PostConsensusSyncCommitteeContributionTooFewRootsMsg = func(sk *bls.SecretKey, id spectypes.OperatorID, keySet *spectestingutils.TestKeySet) *spectypes.PartialSignatureMessages {
	ret := postConsensusSyncCommitteeContributionMsg(sk, id, spectestingutils.TestingValidatorIndex, keySet, false, false, false)
	msg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     spectestingutils.TestingDutySlot,
		Messages: ret.Messages[0:2],
	}

	return msg
}

var PostConsensusWrongSyncCommitteeContributionMsg = func(sk *bls.SecretKey, id spectypes.OperatorID, keySet *spectestingutils.TestKeySet) *spectypes.PartialSignatureMessages {
	return postConsensusSyncCommitteeContributionMsg(sk, id, spectestingutils.TestingValidatorIndex, keySet, true, false, false)
}

var PostConsensusWrongValidatorIndexSyncCommitteeContributionMsg = func(sk *bls.SecretKey, id spectypes.OperatorID, keySet *spectestingutils.TestKeySet) *spectypes.PartialSignatureMessages {
	msg := postConsensusSyncCommitteeContributionMsg(sk, id, spectestingutils.TestingValidatorIndex, keySet, true, false, false)
	for _, m := range msg.Messages {
		m.ValidatorIndex = spectestingutils.TestingWrongValidatorIndex
	}
	return msg
}

var PostConsensusWrongSigSyncCommitteeContributionMsg = func(sk *bls.SecretKey, id spectypes.OperatorID, keySet *spectestingutils.TestKeySet) *spectypes.PartialSignatureMessages {
	return postConsensusSyncCommitteeContributionMsg(sk, id, spectestingutils.TestingValidatorIndex, keySet, false, true, false)
}

var postConsensusSyncCommitteeContributionMsg = func(
	sk *bls.SecretKey,
	id spectypes.OperatorID,
	validatorIndex phase0.ValidatorIndex,
	keySet *spectestingutils.TestKeySet,
	wrongRoot bool,
	wrongBeaconSig bool,
	wrongRootOrder bool,
) *spectypes.PartialSignatureMessages {
	signer := spectestingutils.NewTestingKeyManager()
	beacon := spectestingutils.NewTestingBeaconNode()
	dContribAndProof, _ := beacon.DomainData(1, spectypes.DomainContributionAndProof)

	msgs := make([]*spectypes.PartialSignatureMessage, 0)
	for index := range spectestingutils.TestingSyncCommitteeContributions {
		// sign contrib and proof
		contribAndProof := &altair.ContributionAndProof{
			AggregatorIndex: validatorIndex,
			Contribution:    &spectestingutils.TestingContributionsData[index].Contribution,
			SelectionProof:  spectestingutils.TestingContributionsData[index].SelectionProofSig,
		}

		if wrongRoot {
			contribAndProof.AggregatorIndex = 100
		}

		signed, root, _ := signer.SignBeaconObject(contribAndProof, dContribAndProof, sk.GetPublicKey().Serialize(), spectypes.DomainSyncCommitteeSelectionProof)
		if wrongBeaconSig {
			signed, root, _ = signer.SignBeaconObject(contribAndProof, dContribAndProof, spectestingutils.Testing7SharesSet().ValidatorPK.Serialize(), spectypes.DomainSyncCommitteeSelectionProof)
		}

		msg := &spectypes.PartialSignatureMessage{
			PartialSignature: signed,
			SigningRoot:      root,
			Signer:           id,
			ValidatorIndex:   spectestingutils.TestingValidatorIndex,
		}

		msgs = append(msgs, msg)
	}

	if wrongRootOrder {
		m := msgs[0]
		msgs[0] = msgs[1]
		msgs[1] = m
	}

	msg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     spectestingutils.TestingDutySlot,
		Messages: msgs,
	}

	return msg
}

// ensureRoot ensures that SigningRoot will have sufficient allocated memory
// otherwise we get panic from bls:
// github.com/herumi/bls-eth-go-binary/bls.(*Sign).VerifyByte:738
func ensureRoot(root [32]byte) [32]byte {
	tmp := [32]byte{}
	copy(tmp[:], root[:])
	return tmp
}
