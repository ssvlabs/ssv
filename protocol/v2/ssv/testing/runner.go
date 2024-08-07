package testing

import (
	"bytes"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	specssv "github.com/ssvlabs/ssv-spec/ssv"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/exporter/convert"
	"github.com/ssvlabs/ssv/integration/qbft/tests"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/testing"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
)

var TestingHighestDecidedSlot = phase0.Slot(0)

var CommitteeRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	return baseRunner(logger, spectypes.RoleCommittee, keySet)
}

var CommitteeRunnerWithShareMap = func(logger *zap.Logger, shareMap map[phase0.ValidatorIndex]*spectypes.Share) runner.Runner {
	return baseRunnerWithShareMap(logger, spectypes.RoleCommittee, shareMap)
}

//var AttesterRunner7Operators = func(keySet *spectestingutils.TestKeySet) runner.Runner {
//	return baseRunner(spectypes.BNRoleAttester, specssv.AttesterValueCheckF(spectestingutils.NewTestingKeyManager(), spectypes.BeaconTestNetwork, spectestingutils.TestingValidatorPubKey[:], spectestingutils.TestingValidatorIndex), keySet)
//}

var ProposerRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	return baseRunner(logger, spectypes.RoleProposer, keySet)
}

var AggregatorRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	return baseRunner(logger, spectypes.RoleAggregator, keySet)
}

var SyncCommitteeContributionRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	return baseRunner(logger, spectypes.RoleSyncCommitteeContribution, keySet)
}

var ValidatorRegistrationRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	ret := baseRunner(logger, spectypes.RoleValidatorRegistration, keySet)
	return ret
}

var VoluntaryExitRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	return baseRunner(logger, spectypes.RoleVoluntaryExit, keySet)
}

var UnknownDutyTypeRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	return baseRunner(logger, spectestingutils.UnknownDutyType, keySet)
}

var baseRunner = func(
	logger *zap.Logger,
	role spectypes.RunnerRole,
	keySet *spectestingutils.TestKeySet,
) runner.Runner {
	share := spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex)
	identifier := func() []byte {
		messageID := spectypes.NewMsgID(spectypes.JatoTestnet, spectestingutils.TestingValidatorPubKey[:], role)
		return messageID[:]
	}
	net := spectestingutils.NewTestingNetwork(1, keySet.OperatorKeys[1])
	km := spectestingutils.NewTestingKeyManager()
	operator := spectestingutils.TestingCommitteeMember(keySet)
	opSigner := spectestingutils.NewOperatorSigner(keySet, 1)

	var valCheck specqbft.ProposedValueCheckF
	switch role {
	case spectypes.RoleCommittee:
		valCheck = specssv.BeaconVoteValueCheckF(km, spectestingutils.TestingDutySlot,
			[]spectypes.ShareValidatorPK{share.SharePubKey}, spectestingutils.TestingDutyEpoch)
	case spectypes.RoleProposer:
		valCheck = specssv.ProposerValueCheckF(km, spectypes.BeaconTestNetwork,
			(spectypes.ValidatorPK)(spectestingutils.TestingValidatorPubKey), spectestingutils.TestingValidatorIndex, share.SharePubKey)
	case spectypes.RoleAggregator:
		valCheck = specssv.AggregatorValueCheckF(km, spectypes.BeaconTestNetwork,
			(spectypes.ValidatorPK)(spectestingutils.TestingValidatorPubKey), spectestingutils.TestingValidatorIndex)
	case spectypes.RoleSyncCommitteeContribution:
		valCheck = specssv.SyncCommitteeContributionValueCheckF(km, spectypes.BeaconTestNetwork,
			(spectypes.ValidatorPK)(spectestingutils.TestingValidatorPubKey), spectestingutils.TestingValidatorIndex)
	default:
		valCheck = nil
	}

	config := testing.TestingConfig(logger, keySet, convert.RunnerRole(role))
	config.ValueCheckF = valCheck
	config.ProposerF = func(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
		return 1
	}
	config.Network = net
	config.BeaconSigner = km
	config.Storage = testing.TestingStores(logger).Get(convert.RunnerRole(role))

	contr := testing.NewTestingQBFTController(
		spectestingutils.Testing4SharesSet(),
		identifier,
		operator,
		config,
		false,
	)

	shareMap := make(map[phase0.ValidatorIndex]*spectypes.Share)
	shareMap[share.ValidatorIndex] = share

	switch role {
	case spectypes.RoleCommittee:
		return runner.NewCommitteeRunner(
			networkconfig.TestNetwork,
			shareMap,
			contr,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
			valCheck,
		)
	case spectypes.RoleAggregator:
		return runner.NewAggregatorRunner(
			networkconfig.TestNetwork,
			spectypes.BeaconTestNetwork,
			shareMap,
			contr,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
			valCheck,
			TestingHighestDecidedSlot,
		)
	case spectypes.RoleProposer:
		return runner.NewProposerRunner(
			networkconfig.TestNetwork,
			spectypes.BeaconTestNetwork,
			shareMap,
			contr,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
			valCheck,
			TestingHighestDecidedSlot,
		)
	case spectypes.RoleSyncCommitteeContribution:
		return runner.NewSyncCommitteeAggregatorRunner(
			networkconfig.TestNetwork,
			spectypes.BeaconTestNetwork,
			shareMap,
			contr,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
			valCheck,
			TestingHighestDecidedSlot,
		)
	case spectypes.RoleValidatorRegistration:
		return runner.NewValidatorRegistrationRunner(
			networkconfig.TestNetwork,
			spectypes.BeaconTestNetwork,
			shareMap,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
		)
	case spectypes.RoleVoluntaryExit:
		return runner.NewVoluntaryExitRunner(
			networkconfig.TestNetwork,
			spectypes.BeaconTestNetwork,
			shareMap,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
		)
	case spectestingutils.UnknownDutyType:
		ret := runner.NewCommitteeRunner(
			networkconfig.TestNetwork,
			shareMap,
			contr,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
			valCheck,
		)
		ret.(*runner.CommitteeRunner).BaseRunner.RunnerRoleType = spectestingutils.UnknownDutyType
		return ret
	default:
		panic("unknown role type")
	}
}

//
//var DecidedRunner = func(keySet *spectestingutils.TestKeySet) runner.Runner {
//	return decideRunner(spectestingutils.TestAttesterConsensusData, specqbft.FirstHeight, keySet)
//}
//
//var DecidedRunnerWithHeight = func(height specqbft.Height, keySet *spectestingutils.TestKeySet) runner.Runner {
//	return decideRunner(spectestingutils.TestAttesterConsensusData, height, keySet)
//}
//
//var DecidedRunnerUnknownDutyType = func(keySet *spectestingutils.TestKeySet) runner.Runner {
//	return decideRunner(spectestingutils.TestConsensusUnkownDutyTypeData, specqbft.FirstHeight, keySet)
//}
//
//var decideRunner = func(consensusInput *spectypes.ValidatorConsensusData, height specqbft.Height, keySet *spectestingutils.TestKeySet) runner.Runner {
//	v := BaseValidator(keySet)
//	consensusDataByts, _ := consensusInput.Encode()
//	msgs := DecidingMsgsForHeight(consensusDataByts, []byte{1, 2, 3, 4}, height, keySet)
//
//	if err := v.ValidatorDutyRunners[spectypes.BNRoleAttester].StartNewDuty(consensusInput.Duty); err != nil {
//		panic(err.Error())
//	}
//	for _, msg := range msgs {
//		ssvMsg := SSVMsgAttester(msg, nil)
//		if err := v.ProcessMessage(ssvMsg); err != nil {
//			panic(err.Error())
//		}
//	}
//
//	return v.ValidatorDutyRunners[spectypes.BNRoleAttester]
//}
//
//var DecidingMsgsForHeight = func(consensusData, msgIdentifier []byte, height specqbft.Height, keySet *spectestingutils.TestKeySet) []*spectypes.SignedSSVMessage {
//	msgs := make([]*spectypes.SignedSSVMessage, 0)
//	for h := specqbft.FirstHeight; h <= height; h++ {
//		msgs = append(msgs, spectestingutils.SignQBFTMsg(keySet.Shares[1], 1, &specqbft.Message{
//			MsgType:    specqbft.ProposalMsgType,
//			Height:     h,
//			Round:      specqbft.FirstRound,
//			Identifier: msgIdentifier,
//			Data:       spectestingutils.ProposalDataBytes(consensusData, nil, nil),
//		}))
//
//		// prepare
//		for i := uint64(1); i <= keySet.Threshold; i++ {
//			msgs = append(msgs, spectestingutils.SignQBFTMsg(keySet.Shares[spectypes.OperatorID(i)], spectypes.OperatorID(i), &specqbft.Message{
//				MsgType:    specqbft.PrepareMsgType,
//				Height:     h,
//				Round:      specqbft.FirstRound,
//				Identifier: msgIdentifier,
//				Data:       spectestingutils.PrepareDataBytes(consensusData),
//			}))
//		}
//		// commit
//		for i := uint64(1); i <= keySet.Threshold; i++ {
//			msgs = append(msgs, spectestingutils.SignQBFTMsg(keySet.Shares[spectypes.OperatorID(i)], spectypes.OperatorID(i), &specqbft.Message{
//				MsgType:    specqbft.CommitMsgType,
//				Height:     h,
//				Round:      specqbft.FirstRound,
//				Identifier: msgIdentifier,
//				Data:       spectestingutils.CommitDataBytes(consensusData),
//			}))
//		}
//	}
//	return msgs
//}

var baseRunnerWithShareMap = func(
	logger *zap.Logger,
	role spectypes.RunnerRole,
	shareMap map[phase0.ValidatorIndex]*spectypes.Share,
) runner.Runner {

	var keySetInstance *spectestingutils.TestKeySet
	var shareInstance *spectypes.Share
	for _, share := range shareMap {
		keySetInstance = spectestingutils.KeySetForShare(share)
		break
	}

	sharePubKeys := make([]spectypes.ShareValidatorPK, 0)
	for _, share := range shareMap {
		sharePubKeys = append(sharePubKeys, share.SharePubKey)
	}

	// Identifier
	var ownerID []byte
	if role == spectypes.RoleCommittee {
		committee := make([]uint64, 0)
		for _, op := range keySetInstance.Committee() {
			committee = append(committee, op.Signer)
		}
		committeeID := spectypes.GetCommitteeID(committee)
		ownerID = bytes.Clone(committeeID[:])
	} else {
		ownerID = spectestingutils.TestingValidatorPubKey[:]
	}

	identifier := func() []byte {
		messageID := spectypes.NewMsgID(spectestingutils.TestingSSVDomainType, ownerID, role)
		return messageID[:]
	}

	net := spectestingutils.NewTestingNetwork(1, keySetInstance.OperatorKeys[1])

	km := spectestingutils.NewTestingKeyManager()
	committeeMember := spectestingutils.TestingCommitteeMember(keySetInstance)
	opSigner := spectestingutils.NewOperatorSigner(keySetInstance, committeeMember.OperatorID)

	var valCheck specqbft.ProposedValueCheckF
	switch role {
	case spectypes.RoleCommittee:
		valCheck = specssv.BeaconVoteValueCheckF(km, spectestingutils.TestingDutySlot,
			sharePubKeys, spectestingutils.TestingDutyEpoch)
	case spectypes.RoleProposer:
		valCheck = specssv.ProposerValueCheckF(km, spectypes.BeaconTestNetwork,
			shareInstance.ValidatorPubKey, shareInstance.ValidatorIndex, shareInstance.SharePubKey)
	case spectypes.RoleAggregator:
		valCheck = specssv.AggregatorValueCheckF(km, spectypes.BeaconTestNetwork,
			shareInstance.ValidatorPubKey, shareInstance.ValidatorIndex)
	case spectypes.RoleSyncCommitteeContribution:
		valCheck = specssv.SyncCommitteeContributionValueCheckF(km, spectypes.BeaconTestNetwork,
			shareInstance.ValidatorPubKey, shareInstance.ValidatorIndex)
	default:
		valCheck = nil
	}

	config := testing.TestingConfig(logger, keySetInstance, convert.RunnerRole(role))
	config.ValueCheckF = valCheck
	config.ProposerF = func(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
		return 1
	}
	config.Network = net
	config.Storage = testing.TestingStores(logger).Get(convert.RunnerRole(role))

	contr := testing.NewTestingQBFTController(
		spectestingutils.Testing4SharesSet(),
		identifier,
		committeeMember,
		config,
		false,
	)

	switch role {
	case spectypes.RoleCommittee:
		return runner.NewCommitteeRunner(
			networkconfig.TestNetwork,
			shareMap,
			contr,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
			valCheck,
		)
	case spectypes.RoleAggregator:
		return runner.NewAggregatorRunner(
			networkconfig.TestNetwork,
			spectypes.BeaconTestNetwork,
			shareMap,
			contr,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
			valCheck,
			TestingHighestDecidedSlot,
		)
	case spectypes.RoleProposer:
		return runner.NewProposerRunner(
			networkconfig.TestNetwork,
			spectypes.BeaconTestNetwork,
			shareMap,
			contr,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
			valCheck,
			TestingHighestDecidedSlot,
		)
	case spectypes.RoleSyncCommitteeContribution:
		return runner.NewSyncCommitteeAggregatorRunner(
			networkconfig.TestNetwork,
			spectypes.BeaconTestNetwork,
			shareMap,
			contr,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
			valCheck,
			TestingHighestDecidedSlot,
		)
	case spectypes.RoleValidatorRegistration:
		return runner.NewValidatorRegistrationRunner(
			networkconfig.TestNetwork,
			spectypes.BeaconTestNetwork,
			shareMap,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
		)
	case spectypes.RoleVoluntaryExit:
		return runner.NewVoluntaryExitRunner(
			networkconfig.TestNetwork,
			spectypes.BeaconTestNetwork,
			shareMap,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
		)
	case spectestingutils.UnknownDutyType:
		ret := runner.NewCommitteeRunner(
			networkconfig.TestNetwork,
			shareMap,
			contr,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
			valCheck,
		)
		ret.(*runner.CommitteeRunner).BaseRunner.RunnerRoleType = spectestingutils.UnknownDutyType
		return ret
	default:
		panic("unknown role type")
	}
}
