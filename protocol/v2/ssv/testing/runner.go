package testing

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	specssv "github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v2/qbft/testing"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
)

var TestingHighestDecidedSlot = phase0.Slot(0)

var AttesterRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	return baseRunner(logger, spectypes.BNRoleAttester, specssv.AttesterValueCheckF(spectestingutils.NewTestingKeyManager(), spectypes.BeaconTestNetwork, spectestingutils.TestingValidatorPubKey[:], spectestingutils.TestingValidatorIndex, nil), keySet)
}

//var AttesterRunner7Operators = func(keySet *spectestingutils.TestKeySet) runner.Runner {
//	return baseRunner(spectypes.BNRoleAttester, specssv.AttesterValueCheckF(spectestingutils.NewTestingKeyManager(), spectypes.BeaconTestNetwork, spectestingutils.TestingValidatorPubKey[:], spectestingutils.TestingValidatorIndex), keySet)
//}

var ProposerRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	return baseRunner(logger, spectypes.BNRoleProposer, specssv.ProposerValueCheckF(spectestingutils.NewTestingKeyManager(), spectypes.BeaconTestNetwork, spectestingutils.TestingValidatorPubKey[:], spectestingutils.TestingValidatorIndex, nil), keySet)
}

var ProposerBlindedBlockRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	ret := baseRunner(
		logger,
		spectypes.BNRoleProposer,
		specssv.ProposerValueCheckF(spectestingutils.NewTestingKeyManager(), spectypes.BeaconTestNetwork, spectestingutils.TestingValidatorPubKey[:], spectestingutils.TestingValidatorIndex, nil),
		keySet,
	)
	ret.(*runner.ProposerRunner).ProducesBlindedBlocks = true
	return ret
}

var AggregatorRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	return baseRunner(logger, spectypes.BNRoleAggregator, specssv.AggregatorValueCheckF(spectestingutils.NewTestingKeyManager(), spectypes.BeaconTestNetwork, spectestingutils.TestingValidatorPubKey[:], spectestingutils.TestingValidatorIndex), keySet)
}

var SyncCommitteeRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	return baseRunner(logger, spectypes.BNRoleSyncCommittee, specssv.SyncCommitteeValueCheckF(spectestingutils.NewTestingKeyManager(), spectypes.BeaconTestNetwork, spectestingutils.TestingValidatorPubKey[:], spectestingutils.TestingValidatorIndex), keySet)
}

var SyncCommitteeContributionRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	return baseRunner(logger, spectypes.BNRoleSyncCommitteeContribution, specssv.SyncCommitteeContributionValueCheckF(spectestingutils.NewTestingKeyManager(), spectypes.BeaconTestNetwork, spectestingutils.TestingValidatorPubKey[:], spectestingutils.TestingValidatorIndex), keySet)
}

var ValidatorRegistrationRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	ret := baseRunner(logger, spectypes.BNRoleValidatorRegistration, nil, keySet)
	return ret
}

var VoluntaryExitRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	return baseRunner(logger, spectypes.BNRoleVoluntaryExit, nil, keySet)
}

var UnknownDutyTypeRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	return baseRunner(logger, spectestingutils.UnknownDutyType, spectestingutils.UnknownDutyValueCheck(), keySet)
}

var baseRunner = func(logger *zap.Logger, role spectypes.BeaconRole, valCheck specqbft.ProposedValueCheckF, keySet *spectestingutils.TestKeySet) runner.Runner {
	share := spectestingutils.TestingShare(keySet)
	identifier := spectypes.NewMsgID(TestingSSVDomainType, spectestingutils.TestingValidatorPubKey[:], role)
	net := spectestingutils.NewTestingNetwork()
	km := spectestingutils.NewTestingKeyManager()

	config := testing.TestingConfig(logger, keySet, identifier.GetRoleType())
	config.ValueCheckF = valCheck
	config.ProposerF = func(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
		return 1
	}
	config.Network = net
	config.Signer = km

	contr := testing.NewTestingQBFTController(
		identifier[:],
		share,
		config,
		false,
	)

	switch role {
	case spectypes.BNRoleAttester:
		return runner.NewAttesterRunnner(
			spectypes.BeaconTestNetwork,
			share,
			contr,
			spectestingutils.NewTestingBeaconNode(),
			net,
			km,
			valCheck,
			TestingHighestDecidedSlot,
		)
	case spectypes.BNRoleAggregator:
		return runner.NewAggregatorRunner(
			spectypes.BeaconTestNetwork,
			share,
			contr,
			spectestingutils.NewTestingBeaconNode(),
			net,
			km,
			valCheck,
			TestingHighestDecidedSlot,
		)
	case spectypes.BNRoleProposer:
		return runner.NewProposerRunner(
			spectypes.BeaconTestNetwork,
			share,
			contr,
			spectestingutils.NewTestingBeaconNode(),
			net,
			km,
			valCheck,
			TestingHighestDecidedSlot,
		)
	case spectypes.BNRoleSyncCommittee:
		return runner.NewSyncCommitteeRunner(
			spectypes.BeaconTestNetwork,
			share,
			contr,
			spectestingutils.NewTestingBeaconNode(),
			net,
			km,
			valCheck,
			TestingHighestDecidedSlot,
		)
	case spectypes.BNRoleSyncCommitteeContribution:
		return runner.NewSyncCommitteeAggregatorRunner(
			spectypes.BeaconTestNetwork,
			share,
			contr,
			spectestingutils.NewTestingBeaconNode(),
			net,
			km,
			valCheck,
			TestingHighestDecidedSlot,
		)
	case spectypes.BNRoleValidatorRegistration:
		return runner.NewValidatorRegistrationRunner(
			spectypes.BeaconTestNetwork,
			share,
			contr,
			spectestingutils.NewTestingBeaconNode(),
			net,
			km,
		)
	case spectypes.BNRoleVoluntaryExit:
		return runner.NewVoluntaryExitRunner(
			spectypes.BeaconTestNetwork,
			share,
			spectestingutils.NewTestingBeaconNode(),
			net,
			km,
		)
	case spectestingutils.UnknownDutyType:
		ret := runner.NewAttesterRunnner(
			spectypes.BeaconTestNetwork,
			share,
			contr,
			spectestingutils.NewTestingBeaconNode(),
			net,
			km,
			valCheck,
			TestingHighestDecidedSlot,
		)
		ret.(*runner.AttesterRunner).BaseRunner.BeaconRoleType = spectestingutils.UnknownDutyType
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
//var decideRunner = func(consensusInput *spectypes.ConsensusData, height specqbft.Height, keySet *spectestingutils.TestKeySet) runner.Runner {
//	v := BaseValidator(keySet)
//	consensusDataByts, _ := consensusInput.Encode()
//	msgs := DecidingMsgsForHeight(consensusDataByts, []byte{1, 2, 3, 4}, height, keySet)
//
//	if err := v.DutyRunners[spectypes.BNRoleAttester].StartNewDuty(consensusInput.Duty); err != nil {
//		panic(err.Error())
//	}
//	for _, msg := range msgs {
//		ssvMsg := SSVMsgAttester(msg, nil)
//		if err := v.ProcessMessage(ssvMsg); err != nil {
//			panic(err.Error())
//		}
//	}
//
//	return v.DutyRunners[spectypes.BNRoleAttester]
//}
//
//var DecidingMsgsForHeight = func(consensusData, msgIdentifier []byte, height specqbft.Height, keySet *spectestingutils.TestKeySet) []*specqbft.SignedMessage {
//	msgs := make([]*specqbft.SignedMessage, 0)
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
