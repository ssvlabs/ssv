package testing

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
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
	return baseRunner(logger, spectypes.RoleCommittee, specssv.BeaconVoteValueCheckF(spectestingutils.NewTestingKeyManager(), spectestingutils.TestingDutySlot, nil, spectestingutils.TestingDutyEpoch), keySet)
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
	operator := spectestingutils.TestingOperator(keySet)
	opSigner := spectestingutils.NewTestingOperatorSigner(keySet, 1)

	config := testing.TestingConfig(logger, keySet, identifier.GetRoleType())
	config.ValueCheckF = valCheck
	config.ProposerF = func(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
		return 1
	}
	config.Network = net
	config.BeaconSigner = km
	config.OperatorSigner = opSigner
	config.SignatureVerifier = spectestingutils.NewTestingVerifier()

	contr := specqbft.NewController(
		identifier[:],
		operator,
		config,
	)

	shareMap := make(map[phase0.ValidatorIndex]*spectypes.Share)
	shareMap[share.ValidatorIndex] = share

	switch role {
	case spectypes.RoleCommittee:
		return specssv.NewCommitteeRunner(
			spectypes.BeaconTestNetwork,
			shareMap,
			contr,
			spectestingutils.NewTestingBeaconNode(),
			net,
			km,
			opSigner,
			valCheck,
		).(runner.Runner)
	case spectypes.RoleAggregator:
		return specssv.NewAggregatorRunner(
			spectypes.BeaconTestNetwork,
			shareMap,
			contr,
			spectestingutils.NewTestingBeaconNode(),
			net,
			km,
			opSigner,
			valCheck,
			TestingHighestDecidedSlot,
		).(runner.Runner)
	case spectypes.RoleProposer:
		return specssv.NewProposerRunner(
			spectypes.BeaconTestNetwork,
			shareMap,
			contr,
			spectestingutils.NewTestingBeaconNode(),
			net,
			km,
			opSigner,
			valCheck,
			TestingHighestDecidedSlot,
		).(runner.Runner)
	case spectypes.RoleSyncCommitteeContribution:
		return specssv.NewSyncCommitteeAggregatorRunner(
			spectypes.BeaconTestNetwork,
			shareMap,
			contr,
			spectestingutils.NewTestingBeaconNode(),
			net,
			km,
			opSigner,
			valCheck,
			TestingHighestDecidedSlot,
		).(runner.Runner)
	case spectypes.RoleValidatorRegistration:
		return specssv.NewValidatorRegistrationRunner(
			spectypes.BeaconTestNetwork,
			shareMap,
			spectestingutils.NewTestingBeaconNode(),
			net,
			km,
			opSigner,
		).(runner.Runner)
	case spectypes.RoleVoluntaryExit:
		return specssv.NewVoluntaryExitRunner(
			spectypes.BeaconTestNetwork,
			shareMap,
			spectestingutils.NewTestingBeaconNode(),
			net,
			km,
			opSigner,
		).(runner.Runner)
	case spectestingutils.UnknownDutyType:
		ret := specssv.NewCommitteeRunner(
			spectypes.BeaconTestNetwork,
			shareMap,
			contr,
			spectestingutils.NewTestingBeaconNode(),
			net,
			km,
			opSigner,
			valCheck,
		)
		ret.(*specssv.CommitteeRunner).BaseRunner.RunnerRoleType = spectestingutils.UnknownDutyType
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
