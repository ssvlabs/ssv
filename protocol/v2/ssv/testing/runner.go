package testing

import (
	"bytes"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv/exporter/convert"
	"github.com/ssvlabs/ssv/integration/qbft/tests"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/validator"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/testing"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	specssv "github.com/ssvlabs/ssv-spec/ssv"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
)

var TestingHighestDecidedSlot = phase0.Slot(0)

var CommitteeRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	// TODO fixme ?

	return baseRunner(logger, spectypes.RoleCommittee, validator.TempBeaconVoteValueCheckF(spectestingutils.NewTestingKeyManager(), spectestingutils.TestingDutySlot, nil, spectestingutils.TestingDutyEpoch), keySet)
	//return baseRunner(logger, spectypes.RoleCommittee, specssv.BeaconVoteValueCheckF(spectestingutils.NewTestingKeyManager(), spectestingutils.TestingDutySlot, nil, spectestingutils.TestingDutyEpoch), keySet)
}

var CommitteeRunnerWithShareMap = func(logger *zap.Logger, shareMap map[phase0.ValidatorIndex]*spectypes.Share) runner.Runner {
	// TODO fixme ?
	return baseRunnerWithShareMap(logger, spectypes.RoleCommittee, validator.TempBeaconVoteValueCheckF(spectestingutils.NewTestingKeyManager(), spectestingutils.TestingDutySlot, nil, spectestingutils.TestingDutyEpoch), shareMap)
	//return baseRunnerWithShareMap(logger, spectypes.RoleCommittee, specssv.BeaconVoteValueCheckF(spectestingutils.NewTestingKeyManager(), spectestingutils.TestingDutySlot, nil, spectestingutils.TestingDutyEpoch), shareMap)
}

//var AttesterRunner7Operators = func(keySet *spectestingutils.TestKeySet) runner.Runner {
//	return baseRunner(spectypes.BNRoleAttester, specssv.AttesterValueCheckF(spectestingutils.NewTestingKeyManager(), spectypes.BeaconTestNetwork, spectestingutils.TestingValidatorPubKey[:], spectestingutils.TestingValidatorIndex), keySet)
//}

var ProposerRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	return baseRunner(
		logger,
		spectypes.RoleProposer,
		specssv.ProposerValueCheckF(
			spectestingutils.NewTestingKeyManager(),
			spectypes.BeaconTestNetwork,
			(spectypes.ValidatorPK)(spectestingutils.TestingValidatorPubKey[:]),
			spectestingutils.TestingValidatorIndex,
			nil,
		),
		keySet)
}

var AggregatorRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	return baseRunner(logger, spectypes.RoleAggregator, specssv.AggregatorValueCheckF(spectestingutils.NewTestingKeyManager(), spectypes.BeaconTestNetwork, (spectypes.ValidatorPK)(spectestingutils.TestingValidatorPubKey[:]), spectestingutils.TestingValidatorIndex), keySet)
}

var SyncCommitteeContributionRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	return baseRunner(logger, spectypes.RoleSyncCommitteeContribution, specssv.SyncCommitteeContributionValueCheckF(spectestingutils.NewTestingKeyManager(), spectypes.BeaconTestNetwork, (spectypes.ValidatorPK)(spectestingutils.TestingValidatorPubKey), spectestingutils.TestingValidatorIndex), keySet)
}

var ValidatorRegistrationRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	ret := baseRunner(logger, spectypes.RoleValidatorRegistration, nil, keySet)
	return ret
}

var VoluntaryExitRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	return baseRunner(logger, spectypes.RoleVoluntaryExit, nil, keySet)
}

var UnknownDutyTypeRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	return baseRunner(logger, spectestingutils.UnknownDutyType, spectestingutils.UnknownDutyValueCheck(), keySet)
}

var baseRunner = func(
	logger *zap.Logger,
	role spectypes.RunnerRole,
	valCheck specqbft.ProposedValueCheckF,
	keySet *spectestingutils.TestKeySet,
) runner.Runner {
	share := spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex)
	identifier := spectypes.NewMsgID(TestingSSVDomainType, spectestingutils.TestingValidatorPubKey[:], spectypes.RunnerRole(role))
	net := spectestingutils.NewTestingNetwork(1, keySet.OperatorKeys[1])
	km := spectestingutils.NewTestingKeyManager()
	operator := spectestingutils.TestingCommitteeMember(keySet)
	opSigner := spectestingutils.NewTestingOperatorSigner(keySet, 1)

	config := testing.TestingConfig(logger, keySet, convert.RunnerRole(identifier.GetRoleType()))
	config.ValueCheckF = valCheck
	config.ProposerF = func(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
		return 1
	}
	config.Network = net
	config.BeaconSigner = km
	config.OperatorSigner = opSigner
	config.SignatureVerifier = spectestingutils.NewTestingVerifier()

	contr := testing.NewTestingQBFTController(
		identifier[:],
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
		).(runner.Runner)
	case spectypes.RoleAggregator:
		return runner.NewAggregatorRunner(
			spectypes.BeaconTestNetwork,
			shareMap,
			contr,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
			valCheck,
			TestingHighestDecidedSlot,
		).(runner.Runner)
	case spectypes.RoleProposer:
		return runner.NewProposerRunner(
			spectypes.BeaconTestNetwork,
			shareMap,
			contr,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
			valCheck,
			TestingHighestDecidedSlot,
		).(runner.Runner)
	case spectypes.RoleSyncCommitteeContribution:
		return runner.NewSyncCommitteeAggregatorRunner(
			spectypes.BeaconTestNetwork,
			shareMap,
			contr,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
			valCheck,
			TestingHighestDecidedSlot,
		).(runner.Runner)
	case spectypes.RoleValidatorRegistration:
		return runner.NewValidatorRegistrationRunner(
			spectypes.BeaconTestNetwork,
			shareMap,
			contr,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
		).(runner.Runner)
	case spectypes.RoleVoluntaryExit:
		return runner.NewVoluntaryExitRunner(
			spectypes.BeaconTestNetwork,
			shareMap,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
		).(runner.Runner)
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
		return ret.(runner.Runner)
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
//var decideRunner = func(consensusInput *spectypes.ConsensusData, height specqbft.Height, keySet *spectestingutils.TestKeySet) runner.Runner {
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
	valCheck specqbft.ProposedValueCheckF,
	shareMap map[phase0.ValidatorIndex]*spectypes.Share,
) runner.Runner {

	var keySetInstance *spectestingutils.TestKeySet
	for _, share := range shareMap {
		keySetInstance = spectestingutils.KeySetForShare(share)
		break
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
	identifier := spectypes.NewMsgID(spectestingutils.TestingSSVDomainType, ownerID, role)

	net := spectestingutils.NewTestingNetwork(1, keySetInstance.OperatorKeys[1])

	km := spectestingutils.NewTestingKeyManager()
	committeeMember := spectestingutils.TestingCommitteeMember(keySetInstance)
	opSigner := spectestingutils.NewTestingOperatorSigner(keySetInstance, committeeMember.OperatorID)

	config := testing.TestingConfig(logger, keySetInstance, convert.RunnerRole(identifier.GetRoleType()))
	config.ValueCheckF = valCheck
	config.ProposerF = func(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
		return 1
	}
	config.Network = net
	config.OperatorSigner = opSigner
	config.SignatureVerifier = spectestingutils.NewTestingVerifier()

	contr := testing.NewTestingQBFTController(
		identifier[:],
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
			spectypes.BeaconTestNetwork,
			shareMap,
			contr,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
		)
	case spectypes.RoleVoluntaryExit:
		return runner.NewVoluntaryExitRunner(
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
