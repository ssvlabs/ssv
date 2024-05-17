package testing

import (
	"crypto/sha256"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
<<<<<<< HEAD
	"github.com/attestantio/go-eth2-client/spec/phase0"
=======
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a
	"github.com/herumi/bls-eth-go-binary/bls"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
<<<<<<< HEAD
=======
	"github.com/ssvlabs/ssv-spec/types/testingutils"
>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
)

var TestingSSVDomainType = spectypes.JatoTestnet
<<<<<<< HEAD
var TestingForkData = spectypes.ForkData{Epoch: spectestingutils.TestingDutyEpoch, Domain: TestingSSVDomainType}
var CommitteeMsgID = func() []byte {
	ret := spectypes.NewMsgID(TestingSSVDomainType, spectestingutils.TestingValidatorPubKey[:], spectypes.RoleCommittee)
	return ret[:]
}()
var AttesterMsgID = func() []byte {
	ret := spectypes.NewMsgID(TestingSSVDomainType, spectestingutils.TestingValidatorPubKey[:], spectypes.RoleCommittee)
=======
var AttesterMsgID = func() []byte {
	ret := spectypes.NewMsgID(TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], spectypes.RoleCommittee)
>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a
	return ret[:]
}()
var ProposerMsgID = func() []byte {
<<<<<<< HEAD
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
=======
	ret := spectypes.NewMsgID(TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], spectypes.RoleProposer)
	return ret[:]
}()
var AggregatorMsgID = func() []byte {
	ret := spectypes.NewMsgID(TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], spectypes.RoleAggregator)
	return ret[:]
}()
var SyncCommitteeMsgID = func() []byte {
	ret := spectypes.NewMsgID(TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], spectypes.RoleCommittee)
	return ret[:]
}()
var SyncCommitteeContributionMsgID = func() []byte {
	ret := spectypes.NewMsgID(TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], spectypes.RoleSyncCommitteeContribution)
	return ret[:]
}()
var ValidatorRegistrationMsgID = func() []byte {
	ret := spectypes.NewMsgID(TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], spectypes.RoleValidatorRegistration)
	return ret[:]
}()
var VoluntaryExitMsgID = func() []byte {
	ret := spectypes.NewMsgID(TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], spectypes.RoleVoluntaryExit)
>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a
	return ret[:]
}()

var EncodeConsensusDataTest = func(cd *spectypes.ConsensusData) []byte {
	encodedCD, _ := cd.Encode()
	return encodedCD
}

// BeaconVote data - Committee Runner

var TestBeaconVoteByts, _ = spectestingutils.TestBeaconVote.Encode()

var TestBeaconVoteNextEpochByts, _ = spectestingutils.TestBeaconVoteNextEpoch.Encode()

// ConsensusData - Attester

var TestAttesterConsensusData = &spectypes.ConsensusData{
<<<<<<< HEAD
	Duty:    *spectestingutils.TestingAttesterDuty.BeaconDuties[0],
	DataSSZ: spectestingutils.TestingAttestationDataBytes,
	Version: spec.DataVersionPhase0,
=======
	Duty:    testingutils.TestAttesterConsensusData.Duty,
	DataSSZ: testingutils.TestingAttestationDataBytes,
>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a
}
var TestAttesterConsensusDataByts, _ = TestAttesterConsensusData.Encode()

var TestAttesterNextEpochConsensusData = &spectypes.ConsensusData{
	Duty:    *spectestingutils.TestingAttesterDutyNextEpoch.BeaconDuties[0],
	DataSSZ: spectestingutils.TestingAttestationNextEpochDataBytes,
	Version: spec.DataVersionPhase0,
}

var TestingAttesterNextEpochConsensusDataByts, _ = TestAttesterNextEpochConsensusData.Encode()

// ConsensusData - Aggregator

var TestAggregatorConsensusData = &spectypes.ConsensusData{
	Duty:    spectestingutils.TestingAggregatorDuty,
	DataSSZ: spectestingutils.TestingAggregateAndProofBytes,
	Version: spec.DataVersionPhase0,
}
var TestAggregatorConsensusDataByts, _ = TestAggregatorConsensusData.Encode()

var TestAttesterWithJustificationsConsensusData = func(ks *spectestingutils.TestKeySet) *spectypes.ConsensusData {
	justif := make([]*spectypes.PartialSignatureMessages, 0)
	for i := uint64(1); i <= ks.Threshold; i++ {
		justif = append(justif, PreConsensusRandaoMsg(ks.Shares[i], i))
	}

	return &spectypes.ConsensusData{
		Duty:                       *spectestingutils.TestingAttesterDuty.BeaconDuties[0],
		Version:                    spec.DataVersionDeneb,
		PreConsensusJustifications: justif,
		DataSSZ:                    spectestingutils.TestingAttestationDataBytes,
	}
}

var TestAggregatorWithJustificationsConsensusData = func(ks *spectestingutils.TestKeySet) *spectypes.ConsensusData {
	justif := make([]*spectypes.PartialSignatureMessages, 0)
	for i := uint64(1); i <= ks.Threshold; i++ {
		justif = append(justif, PreConsensusSelectionProofMsg(ks.Shares[i], ks.Shares[i], i, i))
	}

	return &spectypes.ConsensusData{
		Duty:                       spectestingutils.TestingAggregatorDuty,
		Version:                    spec.DataVersionBellatrix,
		PreConsensusJustifications: justif,
		DataSSZ:                    spectestingutils.TestingAggregateAndProofBytes,
	}

}

// ConsensusData - Sync Committee

// TestSyncCommitteeWithJustificationsConsensusData is an invalid sync committee msg (doesn't have pre-consensus)
var TestSyncCommitteeWithJustificationsConsensusData = func(ks *spectestingutils.TestKeySet) *spectypes.ConsensusData {
	justif := make([]*spectypes.PartialSignatureMessages, 0)
	for i := uint64(0); i <= ks.Threshold; i++ {
		justif = append(justif, PreConsensusRandaoMsg(ks.Shares[i+1], i+1))
	}

	return &spectypes.ConsensusData{
		Duty:                       *spectestingutils.TestingSyncCommitteeDuty.BeaconDuties[0],
		Version:                    spec.DataVersionDeneb,
		PreConsensusJustifications: justif,
		DataSSZ:                    spectestingutils.TestingSyncCommitteeBlockRoot[:],
	}
}

var TestSyncCommitteeConsensusData = &spectypes.ConsensusData{
<<<<<<< HEAD
	Duty:    *spectestingutils.TestingSyncCommitteeDuty.BeaconDuties[0],
	DataSSZ: spectestingutils.TestingSyncCommitteeBlockRoot[:],
	Version: spec.DataVersionPhase0,
=======
	Duty:    testingutils.TestingSyncCommitteeContributionDuty,
	DataSSZ: testingutils.TestingSyncCommitteeBlockRoot[:],
>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a
}
var TestSyncCommitteeConsensusDataByts, _ = TestSyncCommitteeConsensusData.Encode()

var TestSyncCommitteeNextEpochConsensusData = &spectypes.ConsensusData{
	Duty:    *spectestingutils.TestingSyncCommitteeDutyNextEpoch.BeaconDuties[0],
	DataSSZ: spectestingutils.TestingSyncCommitteeBlockRoot[:],
	Version: spec.DataVersionPhase0,
}

var TestSyncCommitteeNextEpochConsensusDataByts, _ = TestSyncCommitteeNextEpochConsensusData.Encode()

// ConsensusData - Sync Committee Contribution

var TestSyncCommitteeContributionConsensusData = &spectypes.ConsensusData{
	Duty:    spectestingutils.TestingSyncCommitteeContributionDuty,
	DataSSZ: spectestingutils.TestingContributionsDataBytes,
	Version: spec.DataVersionPhase0,
}
var TestSyncCommitteeContributionConsensusDataByts, _ = TestSyncCommitteeContributionConsensusData.Encode()
var TestSyncCommitteeContributionConsensusDataRoot = func() [32]byte {
	return sha256.Sum256(TestSyncCommitteeContributionConsensusDataByts)
}()

var TestConsensusUnkownDutyTypeData = &spectypes.ConsensusData{
	Duty:    spectestingutils.TestingUnknownDutyType,
	DataSSZ: spectestingutils.TestingAttestationDataBytes,
	Version: spec.DataVersionPhase0,
}
var TestConsensusUnkownDutyTypeDataByts, _ = TestConsensusUnkownDutyTypeData.Encode()

var TestConsensusWrongDutyPKData = &spectypes.ConsensusData{
	Duty:    spectestingutils.TestingWrongDutyPK,
	DataSSZ: spectestingutils.TestingAttestationDataBytes,
	Version: spec.DataVersionPhase0,
}
var TestConsensusWrongDutyPKDataByts, _ = TestConsensusWrongDutyPKData.Encode()

var SSVMsgAttester = func(qbftMsg *spectypes.SignedSSVMessage, partialSigMsg *spectypes.PartialSignatureMessages) *spectypes.SSVMessage {
<<<<<<< HEAD
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

=======
	return ssvMsg(qbftMsg, partialSigMsg, spectypes.NewMsgID(TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], spectypes.RoleCommittee))
}

var SSVMsgWrongID = func(qbftMsg *spectypes.SignedSSVMessage, partialSigMsg *spectypes.PartialSignatureMessages) *spectypes.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, spectypes.NewMsgID(TestingSSVDomainType, testingutils.TestingWrongValidatorPubKey[:], spectypes.RoleCommittee))
}

var SSVMsgProposer = func(qbftMsg *spectypes.SignedSSVMessage, partialSigMsg *spectypes.PartialSignatureMessages) *spectypes.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, spectypes.NewMsgID(TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], spectypes.RoleProposer))
}

var SSVMsgAggregator = func(qbftMsg *spectypes.SignedSSVMessage, partialSigMsg *spectypes.PartialSignatureMessages) *spectypes.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, spectypes.NewMsgID(TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], spectypes.RoleAggregator))
}

var SSVMsgSyncCommittee = func(qbftMsg *spectypes.SignedSSVMessage, partialSigMsg *spectypes.PartialSignatureMessages) *spectypes.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, spectypes.NewMsgID(TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], spectypes.RoleCommittee))
}

var SSVMsgSyncCommitteeContribution = func(qbftMsg *spectypes.SignedSSVMessage, partialSigMsg *spectypes.PartialSignatureMessages) *spectypes.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, spectypes.NewMsgID(TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], spectypes.RoleSyncCommitteeContribution))
}

var SSVMsgValidatorRegistration = func(qbftMsg *spectypes.SignedSSVMessage, partialSigMsg *spectypes.PartialSignatureMessages) *spectypes.SSVMessage {
	return ssvMsg(qbftMsg, partialSigMsg, spectypes.NewMsgID(TestingSSVDomainType, testingutils.TestingValidatorPubKey[:], spectypes.RoleValidatorRegistration))
}

var ssvMsg = func(qbftMsg *spectypes.SignedSSVMessage, postMsg *spectypes.PartialSignatureMessages, msgID spectypes.MessageID) *spectypes.SSVMessage {
	var msgType spectypes.MsgType
	var data []byte
	var err error
>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a
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
<<<<<<< HEAD
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
=======
	return postConsensusAttestationMsg(sk, id, height, true, false, spectestingutils.TestingValidatorIndex)
}

var PostConsensusWrongSigAttestationMsg = func(sk *bls.SecretKey, id spectypes.OperatorID, height specqbft.Height) *spectypes.PartialSignatureMessages {
	return postConsensusAttestationMsg(sk, id, height, false, true, spectestingutils.TestingValidatorIndex)
}

var PostConsensusSigAttestationWrongBeaconSignerMsg = func(sk *bls.SecretKey, id, beaconSigner spectypes.OperatorID, height specqbft.Height) *spectypes.PartialSignatureMessages {
	ret := postConsensusAttestationMsg(sk, beaconSigner, height, false, true, spectestingutils.TestingValidatorIndex)
	ret.Messages[0].Signer = id
	return ret
}

var PostConsensusAttestationMsg = func(sk *bls.SecretKey, id spectypes.OperatorID, height specqbft.Height) *spectypes.PartialSignatureMessages {
	return postConsensusAttestationMsg(sk, id, height, false, false, spectestingutils.TestingValidatorIndex)
}

var PostConsensusAttestationTooManyRootsMsg = func(sk *bls.SecretKey, id spectypes.OperatorID, height specqbft.Height) *spectypes.PartialSignatureMessages {
	ret := postConsensusAttestationMsg(sk, id, height, false, false, spectestingutils.TestingValidatorIndex)
>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a
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
<<<<<<< HEAD
) *spectypes.PartialSignatureMessages {
	signer := spectestingutils.NewTestingKeyManager()
	beacon := spectestingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(spectestingutils.TestingAttestationData.Target.Epoch, spectypes.DomainAttester)
=======
	validatorIndex phase0.ValidatorIndex,
) *spectypes.PartialSignatureMessages {
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(testingutils.TestingAttestationData.Target.Epoch, spectypes.DomainAttester)
>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a

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
<<<<<<< HEAD
				ValidatorIndex:   spectestingutils.TestingValidatorIndex,
=======
				ValidatorIndex:   validatorIndex,
>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a
			},
		},
	}
	return &msgs
<<<<<<< HEAD
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
=======
}

var PostConsensusProposerMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return postConsensusBeaconBlockMsg(sk, id, false, false)
}

var PostConsensusProposerTooManyRootsMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	ret := postConsensusBeaconBlockMsg(sk, id, false, false)
>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a
	ret.Messages = append(ret.Messages, ret.Messages[0])

	msg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
<<<<<<< HEAD
		Slot:     spectestingutils.TestingDutySlot,
=======
		Slot:     testingutils.TestingDutySlot,
>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a
		Messages: ret.Messages,
	}
	return msg
}

<<<<<<< HEAD
var PostConsensusAttestationAndSyncCommitteeMsgTooFewRootsMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
=======
var PostConsensusProposerTooFewRootsMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a
	msg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     spectestingutils.TestingDutySlot,
		Messages: []*spectypes.PartialSignatureMessage{},
	}
	return msg
}

<<<<<<< HEAD
var postConsensusAttestationAndSyncCommitteeMsg = func(
=======
var PostConsensusWrongProposerMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return postConsensusBeaconBlockMsg(sk, id, true, false)
}

var PostConsensusWrongSigProposerMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return postConsensusBeaconBlockMsg(sk, id, false, true)
}

var PostConsensusSigProposerWrongBeaconSignerMsg = func(sk *bls.SecretKey, id, beaconSigner spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	ret := postConsensusBeaconBlockMsg(sk, beaconSigner, false, true)
	ret.Messages[0].Signer = id
	return ret
}

var postConsensusBeaconBlockMsg = func(
>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a
	sk *bls.SecretKey,
	id spectypes.OperatorID,
	height specqbft.Height,
	wrongRoot bool,
	wrongBeaconSig bool,
) *spectypes.PartialSignatureMessages {
<<<<<<< HEAD
	attestationPSigMsg := postConsensusAttestationMsg(sk, id, height, wrongRoot, wrongBeaconSig)
	syncCommitteePSigMessage := postConsensusSyncCommitteeMsg(sk, id, wrongRoot, wrongBeaconSig)
=======
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()
>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a

	attestationPSigMsg.Messages = append(attestationPSigMsg.Messages, syncCommitteePSigMessage.Messages...)

<<<<<<< HEAD
	return attestationPSigMsg
}

var PreConsensusFailedMsg = func(msgSigner *bls.SecretKey, msgSignerID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	signer := spectestingutils.NewTestingKeyManager()
	beacon := spectestingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(spectestingutils.TestingDutyEpoch, spectypes.DomainRandao)
	signed, root, _ := signer.SignBeaconObject(spectypes.SSZUint64(spectestingutils.TestingDutyEpoch), d, msgSigner.GetPublicKey().Serialize(), spectypes.DomainRandao)
=======
	d, _ := beacon.DomainData(1, spectypes.DomainProposer) // epoch doesn't matter here, hard coded
	sig, root, _ := signer.SignBeaconObject(block, d, sk.GetPublicKey().Serialize(), spectypes.DomainProposer)
	if wrongBeaconSig {
		sig, root, _ = signer.SignBeaconObject(block, d, testingutils.Testing7SharesSet().ValidatorPK.Serialize(), spectypes.DomainProposer)
	}
	blsSig := spec.BLSSignature{}
	copy(blsSig[:], sig)

	signed := deneb.SignedBeaconBlock{
		Message:   block.Block,
		Signature: blsSig,
	}

	msgs := spectypes.PartialSignatureMessages{
		Type: spectypes.PostConsensusPartialSig,
		Slot: testingutils.TestingDutySlot,
		Messages: []*spectypes.PartialSignatureMessage{
			{
				PartialSignature: signed.Signature[:],
				SigningRoot:      root,
				Signer:           id,
			},
		},
	}
	return &msgs
}

var PreConsensusFailedMsg = func(msgSigner *bls.SecretKey, msgSignerID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(testingutils.TestingDutyEpoch, spectypes.DomainRandao)
	signed, root, _ := signer.SignBeaconObject(spectypes.SSZUint64(testingutils.TestingDutyEpoch), d, msgSigner.GetPublicKey().Serialize(), spectypes.DomainRandao)
>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a

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
<<<<<<< HEAD
	return randaoMsg(sk, id, false, spectestingutils.TestingDutyEpoch, 1, false)
=======
	return randaoMsg(sk, id, false, testingutils.TestingDutyEpoch, 1, false)
}

// PreConsensusRandaoNextEpochMsg testing for a second duty start
var PreConsensusRandaoNextEpochMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return randaoMsg(sk, id, false, testingutils.TestingDutyEpoch+1, 1, false)
}

var PreConsensusRandaoDifferentEpochMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return randaoMsg(sk, id, false, testingutils.TestingDutyEpoch+1, 1, false)
}

var PreConsensusRandaoTooManyRootsMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return randaoMsg(sk, id, false, testingutils.TestingDutyEpoch, 2, false)
}

var PreConsensusRandaoTooFewRootsMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return randaoMsg(sk, id, false, testingutils.TestingDutyEpoch, 0, false)
}

var PreConsensusRandaoNoMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return randaoMsg(sk, id, false, testingutils.TestingDutyEpoch, 0, false)
}

var PreConsensusRandaoWrongBeaconSigMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return randaoMsg(sk, id, false, testingutils.TestingDutyEpoch, 1, true)
}

var PreConsensusRandaoDifferentSignerMsg = func(
	msgSigner, randaoSigner *bls.SecretKey,
	msgSignerID,
	randaoSignerID spectypes.OperatorID,
) *spectypes.PartialSignatureMessages {
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(testingutils.TestingDutyEpoch, spectypes.DomainRandao)
	signed, root, _ := signer.SignBeaconObject(spectypes.SSZUint64(testingutils.TestingDutyEpoch), d, randaoSigner.GetPublicKey().Serialize(), spectypes.DomainRandao)

	msg := spectypes.PartialSignatureMessages{
		Type: spectypes.RandaoPartialSig,
		Slot: testingutils.TestingDutySlot,
		Messages: []*spectypes.PartialSignatureMessage{
			{
				PartialSignature: signed[:],
				SigningRoot:      root,
				Signer:           randaoSignerID,
			},
		},
	}
	return &msg
>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a
}

var randaoMsg = func(
	sk *bls.SecretKey,
	id spectypes.OperatorID,
	wrongRoot bool,
	epoch phase0.Epoch,
	msgCnt int,
	wrongBeaconSig bool,
) *spectypes.PartialSignatureMessages {
<<<<<<< HEAD
	signer := spectestingutils.NewTestingKeyManager()
	beacon := spectestingutils.NewTestingBeaconNode()
=======
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()
>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a
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
<<<<<<< HEAD

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

var TestSelectionProofWithJustificationsConsensusData = func(ks *spectestingutils.TestKeySet) *spectypes.ConsensusData {
	justif := make([]*spectypes.PartialSignatureMessages, 0)
	for i := uint64(0); i <= ks.Threshold; i++ {
		justif = append(justif, PreConsensusSelectionProofMsg(ks.Shares[i+1], ks.Shares[i+1], i+1, i+1))
	}

	return &spectypes.ConsensusData{
		Duty:                       spectestingutils.TestingAggregatorDuty,
		Version:                    spec.DataVersionDeneb,
		PreConsensusJustifications: justif,
		DataSSZ:                    spectestingutils.TestingAggregateAndProofBytes,
	}
=======
	return &msgs
}

var PreConsensusSelectionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return PreConsensusCustomSlotSelectionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot)
}

var PreConsensusSelectionProofWrongBeaconSigMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return selectionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot, testingutils.TestingDutySlot, 1, true)
}

var PreConsensusSelectionProofNextEpochMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return selectionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot2, testingutils.TestingDutySlot2, 1, false)
}

var PreConsensusSelectionProofTooManyRootsMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return selectionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot, testingutils.TestingDutySlot, 3, false)
}

var PreConsensusSelectionProofTooFewRootsMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return selectionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot, testingutils.TestingDutySlot, 0, false)
}

var PreConsensusCustomSlotSelectionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID, slot spec.Slot) *spectypes.PartialSignatureMessages {
	return selectionProofMsg(msgSK, beaconSK, msgID, beaconID, slot, testingutils.TestingDutySlot, 1, false)
}

var PreConsensusWrongMsgSlotSelectionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return selectionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot, testingutils.TestingDutySlot+1, 1, false)
>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a
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
<<<<<<< HEAD
	signer := spectestingutils.NewTestingKeyManager()
	beacon := spectestingutils.NewTestingBeaconNode()
=======
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()
>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a
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

<<<<<<< HEAD
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
=======
var PreConsensusValidatorRegistrationMsg = func(msgSK *bls.SecretKey, msgID spectypes.OperatorID) *spectypes.PartialSignatureMessage {
	return validatorRegistrationMsg(msgSK, msgSK, msgID, msgID, 1, false, testingutils.TestingDutyEpoch, false)
}

var PreConsensusValidatorRegistrationTooFewRootsMsg = func(msgSK *bls.SecretKey, msgID spectypes.OperatorID) *spectypes.PartialSignatureMessage {
	return validatorRegistrationMsg(msgSK, msgSK, msgID, msgID, 0, false, testingutils.TestingDutyEpoch, false)
}

var PreConsensusValidatorRegistrationTooManyRootsMsg = func(msgSK *bls.SecretKey, msgID spectypes.OperatorID) *spectypes.PartialSignatureMessage {
	return validatorRegistrationMsg(msgSK, msgSK, msgID, msgID, 2, false, testingutils.TestingDutyEpoch, false)
}

var PreConsensusValidatorRegistrationDifferentEpochMsg = func(msgSK *bls.SecretKey, msgID spectypes.OperatorID) *spectypes.PartialSignatureMessage {
	return validatorRegistrationMsg(msgSK, msgSK, msgID, msgID, 1, true, testingutils.TestingDutyEpoch, false)
>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a
}

var validatorRegistrationMsg = func(
	sk, beaconSK *bls.SecretKey,
	id, beaconID spectypes.OperatorID,
	msgCnt int,
	wrongRoot bool,
	slot phase0.Slot,
	wrongBeaconSig bool,
<<<<<<< HEAD
) *spectypes.PartialSignatureMessages {
	signer := spectestingutils.NewTestingKeyManager()
	beacon := spectestingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(spectestingutils.TestingDutyEpoch, spectypes.DomainApplicationBuilder)
=======
) *spectypes.PartialSignatureMessage {
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()
	d, _ := beacon.DomainData(epoch, spectypes.DomainApplicationBuilder)
>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a

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

<<<<<<< HEAD
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

=======
	return msg
}

var PostConsensusAggregatorMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return postConsensusAggregatorMsg(sk, id, false, false)
}

var PostConsensusAggregatorTooManyRootsMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	ret := postConsensusAggregatorMsg(sk, id, false, false)
	ret.Messages = append(ret.Messages, ret.Messages[0])

	msg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     testingutils.TestingDutySlot,
		Messages: ret.Messages,
	}

	return msg
}

var PostConsensusAggregatorTooFewRootsMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	msg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     testingutils.TestingDutySlot,
		Messages: []*spectypes.PartialSignatureMessage{},
	}

	return msg
}

>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a
var PostConsensusWrongAggregatorMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return postConsensusAggregatorMsg(sk, id, true, false)
}

<<<<<<< HEAD
var PostConsensusWrongValidatorIndexAggregatorMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	msg := postConsensusAggregatorMsg(sk, id, true, false)
	for _, m := range msg.Messages {
		m.ValidatorIndex = spectestingutils.TestingWrongValidatorIndex
	}
	return msg
}

var PostConsensusWrongSigAggregatorMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return postConsensusAggregatorMsg(sk, id, false, true)
=======
var PostConsensusWrongSigAggregatorMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return postConsensusAggregatorMsg(sk, id, false, true)
}

var PostConsensusSigAggregatorWrongBeaconSignerMsg = func(sk *bls.SecretKey, id, beaconSigner spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	ret := postConsensusAggregatorMsg(sk, beaconSigner, false, true)
	ret.Messages[0].Signer = id
	return ret
>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a
}

var postConsensusAggregatorMsg = func(
	sk *bls.SecretKey,
	id spectypes.OperatorID,
	wrongRoot bool,
	wrongBeaconSig bool,
) *spectypes.PartialSignatureMessages {
<<<<<<< HEAD
	signer := spectestingutils.NewTestingKeyManager()
	beacon := spectestingutils.NewTestingBeaconNode()
=======
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()
>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a
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
<<<<<<< HEAD
		Slot:     spectestingutils.TestingDutySlot,
		Messages: ret.Messages,
	}
=======
		Slot:     testingutils.TestingDutySlot,
		Messages: ret.Messages,
	}

>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a
	return msg
}

var PostConsensusSyncCommitteeTooFewRootsMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	msg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     spectestingutils.TestingDutySlot,
		Messages: []*spectypes.PartialSignatureMessage{},
	}
<<<<<<< HEAD
=======

>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a
	return msg
}

var PostConsensusWrongSyncCommitteeMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return postConsensusSyncCommitteeMsg(sk, id, true, false)
}

<<<<<<< HEAD
var PostConsensusWrongValidatorIndexSyncCommitteeMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	msg := postConsensusSyncCommitteeMsg(sk, id, true, false)
	for _, m := range msg.Messages {
		m.ValidatorIndex = spectestingutils.TestingWrongValidatorIndex
	}
	return msg
}

var PostConsensusWrongSigSyncCommitteeMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return postConsensusSyncCommitteeMsg(sk, id, false, true)
=======
var PostConsensusWrongSigSyncCommitteeMsg = func(sk *bls.SecretKey, id spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return postConsensusSyncCommitteeMsg(sk, id, false, true)
}

var PostConsensusSigSyncCommitteeWrongBeaconSignerMsg = func(sk *bls.SecretKey, id, beaconSigner spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	ret := postConsensusSyncCommitteeMsg(sk, beaconSigner, false, true)
	ret.Messages[0].Signer = id
	return ret
>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a
}

var postConsensusSyncCommitteeMsg = func(
	sk *bls.SecretKey,
	id spectypes.OperatorID,
	wrongRoot bool,
	wrongBeaconSig bool,
) *spectypes.PartialSignatureMessages {
<<<<<<< HEAD
	signer := spectestingutils.NewTestingKeyManager()
	beacon := spectestingutils.NewTestingBeaconNode()
=======
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()
>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a
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
<<<<<<< HEAD
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
var TestContributionProofWithJustificationsConsensusData = func(ks *spectestingutils.TestKeySet) *spectypes.ConsensusData {
	justif := make([]*spectypes.PartialSignatureMessages, 0)
	for i := uint64(0); i <= ks.Threshold; i++ {
		justif = append(justif, PreConsensusContributionProofMsg(ks.Shares[i+1], ks.Shares[i+1], i+1, i+1))
	}

	return &spectypes.ConsensusData{
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
=======
}

var PreConsensusContributionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return PreConsensusCustomSlotContributionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot)
}

var PreConsensusContributionProofWrongBeaconSigMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return contributionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot, testingutils.TestingDutySlot+1, false, true)
}

var PreConsensusContributionProofNextEpochMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return contributionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot2, testingutils.TestingDutySlot2, false, false)
}

var PreConsensusCustomSlotContributionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID, slot spec.Slot) *spectypes.PartialSignatureMessages {
	return contributionProofMsg(msgSK, beaconSK, msgID, beaconID, slot, testingutils.TestingDutySlot, false, false)
}

var PreConsensusWrongMsgSlotContributionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return contributionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot, testingutils.TestingDutySlot+1, false, false)
}

var PreConsensusWrongOrderContributionProofMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	return contributionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot, testingutils.TestingDutySlot, true, false)
}

var PreConsensusContributionProofTooManyRootsMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	ret := contributionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot, testingutils.TestingDutySlot, false, false)
	msg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.ContributionProofs,
		Slot:     testingutils.TestingDutySlot,
>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a
		Messages: append(ret.Messages, ret.Messages[0]),
	}
	return msg
}

var PreConsensusContributionProofTooFewRootsMsg = func(msgSK, beaconSK *bls.SecretKey, msgID, beaconID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
<<<<<<< HEAD
	ret := contributionProofMsg(msgSK, beaconSK, msgID, beaconID, spectestingutils.TestingDutySlot, spectestingutils.TestingDutySlot, false, false)
	msg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.ContributionProofs,
		Slot:     spectestingutils.TestingDutySlot,
		Messages: ret.Messages[0:2],
	}
=======
	ret := contributionProofMsg(msgSK, beaconSK, msgID, beaconID, testingutils.TestingDutySlot, testingutils.TestingDutySlot, false, false)
	msg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.ContributionProofs,
		Slot:     testingutils.TestingDutySlot,
		Messages: ret.Messages[0:2],
	}

>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a
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
<<<<<<< HEAD
	signer := spectestingutils.NewTestingKeyManager()
	beacon := spectestingutils.NewTestingBeaconNode()
=======
	signer := testingutils.NewTestingKeyManager()
	beacon := testingutils.NewTestingBeaconNode()
>>>>>>> 0a3e03dafe04c6a7dd3fd6efe898523515bdf34a
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
