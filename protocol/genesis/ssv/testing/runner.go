package testing

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspecssv "github.com/ssvlabs/ssv-spec-pre-cc/ssv"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	spectestingutils "github.com/ssvlabs/ssv-spec-pre-cc/types/testingutils"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/genesis/qbft/testing"
	"github.com/ssvlabs/ssv/protocol/genesis/ssv/runner"
)

var TestingHighestDecidedSlot = phase0.Slot(0)

var AttesterRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	return baseRunner(logger, genesisspectypes.BNRoleAttester, genesisspecssv.AttesterValueCheckF(spectestingutils.NewTestingKeyManager(), genesisspectypes.BeaconTestNetwork, spectestingutils.TestingValidatorPubKey[:], spectestingutils.TestingValidatorIndex, nil), keySet)
}

//var AttesterRunner7Operators = func(keySet *spectestingutils.TestKeySet) runner.Runner {
//	return baseRunner(genesisspectypes.BNRoleAttester, genesisspecssv.AttesterValueCheckF(spectestingutils.NewTestingKeyManager(), genesisspectypes.BeaconTestNetwork, spectestingutils.TestingValidatorPubKey[:], spectestingutils.TestingValidatorIndex), keySet)
//}

var ProposerRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	return baseRunner(logger, genesisspectypes.BNRoleProposer, genesisspecssv.ProposerValueCheckF(spectestingutils.NewTestingKeyManager(), genesisspectypes.BeaconTestNetwork, spectestingutils.TestingValidatorPubKey[:], spectestingutils.TestingValidatorIndex, nil), keySet)
}

var ProposerBlindedBlockRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	ret := baseRunner(
		logger,
		genesisspectypes.BNRoleProposer,
		genesisspecssv.ProposerValueCheckF(spectestingutils.NewTestingKeyManager(), genesisspectypes.BeaconTestNetwork, spectestingutils.TestingValidatorPubKey[:], spectestingutils.TestingValidatorIndex, nil),
		keySet,
	)
	ret.(*runner.ProposerRunner).ProducesBlindedBlocks = true
	return ret
}

var AggregatorRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	return baseRunner(logger, genesisspectypes.BNRoleAggregator, genesisspecssv.AggregatorValueCheckF(spectestingutils.NewTestingKeyManager(), genesisspectypes.BeaconTestNetwork, spectestingutils.TestingValidatorPubKey[:], spectestingutils.TestingValidatorIndex), keySet)
}

var SyncCommitteeRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	return baseRunner(logger, genesisspectypes.BNRoleSyncCommittee, genesisspecssv.SyncCommitteeValueCheckF(spectestingutils.NewTestingKeyManager(), genesisspectypes.BeaconTestNetwork, spectestingutils.TestingValidatorPubKey[:], spectestingutils.TestingValidatorIndex), keySet)
}

var SyncCommitteeContributionRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	return baseRunner(logger, genesisspectypes.BNRoleSyncCommitteeContribution, genesisspecssv.SyncCommitteeContributionValueCheckF(spectestingutils.NewTestingKeyManager(), genesisspectypes.BeaconTestNetwork, spectestingutils.TestingValidatorPubKey[:], spectestingutils.TestingValidatorIndex), keySet)
}

var ValidatorRegistrationRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	ret := baseRunner(logger, genesisspectypes.BNRoleValidatorRegistration, nil, keySet)
	return ret
}

var VoluntaryExitRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	return baseRunner(logger, genesisspectypes.BNRoleVoluntaryExit, nil, keySet)
}

var UnknownDutyTypeRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	return baseRunner(logger, spectestingutils.UnknownDutyType, spectestingutils.UnknownDutyValueCheck(), keySet)
}

var baseRunner = func(logger *zap.Logger, role genesisspectypes.BeaconRole, valCheck genesisspecqbft.ProposedValueCheckF, keySet *spectestingutils.TestKeySet) runner.Runner {
	share := spectestingutils.TestingShare(keySet)
	identifier := genesisspectypes.NewMsgID(TestingSSVDomainType, spectestingutils.TestingValidatorPubKey[:], role)
	net := spectestingutils.NewTestingNetwork()
	km := spectestingutils.NewTestingKeyManager()

	config := testing.TestingConfig(logger, keySet, identifier.GetRoleType())
	config.ValueCheckF = valCheck
	config.ProposerF = func(state *genesisspecqbft.State, round genesisspecqbft.Round) genesisspectypes.OperatorID {
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
	case genesisspectypes.BNRoleAttester:
		return runner.NewAttesterRunnner(
			genesisspectypes.BeaconTestNetwork,
			share,
			contr,
			spectestingutils.NewTestingBeaconNode(),
			net,
			km,
			valCheck,
			TestingHighestDecidedSlot,
		)
	case genesisspectypes.BNRoleAggregator:
		return runner.NewAggregatorRunner(
			genesisspectypes.BeaconTestNetwork,
			share,
			contr,
			spectestingutils.NewTestingBeaconNode(),
			net,
			km,
			valCheck,
			TestingHighestDecidedSlot,
		)
	case genesisspectypes.BNRoleProposer:
		return runner.NewProposerRunner(
			genesisspectypes.BeaconTestNetwork,
			share,
			contr,
			spectestingutils.NewTestingBeaconNode(),
			net,
			km,
			valCheck,
			TestingHighestDecidedSlot,
		)
	case genesisspectypes.BNRoleSyncCommittee:
		return runner.NewSyncCommitteeRunner(
			genesisspectypes.BeaconTestNetwork,
			share,
			contr,
			spectestingutils.NewTestingBeaconNode(),
			net,
			km,
			valCheck,
			TestingHighestDecidedSlot,
		)
	case genesisspectypes.BNRoleSyncCommitteeContribution:
		return runner.NewSyncCommitteeAggregatorRunner(
			genesisspectypes.BeaconTestNetwork,
			share,
			contr,
			spectestingutils.NewTestingBeaconNode(),
			net,
			km,
			valCheck,
			TestingHighestDecidedSlot,
		)
	case genesisspectypes.BNRoleValidatorRegistration:
		return runner.NewValidatorRegistrationRunner(
			genesisspectypes.BeaconTestNetwork,
			share,
			contr,
			spectestingutils.NewTestingBeaconNode(),
			net,
			km,
		)
	case genesisspectypes.BNRoleVoluntaryExit:
		return runner.NewVoluntaryExitRunner(
			genesisspectypes.BeaconTestNetwork,
			share,
			spectestingutils.NewTestingBeaconNode(),
			net,
			km,
		)
	case spectestingutils.UnknownDutyType:
		ret := runner.NewAttesterRunnner(
			genesisspectypes.BeaconTestNetwork,
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
//	return decideRunner(spectestingutils.TestAttesterConsensusData, genesisspecqbft.FirstHeight, keySet)
//}
//
//var DecidedRunnerWithHeight = func(height genesisspecqbft.Height, keySet *spectestingutils.TestKeySet) runner.Runner {
//	return decideRunner(spectestingutils.TestAttesterConsensusData, height, keySet)
//}
//
//var DecidedRunnerUnknownDutyType = func(keySet *spectestingutils.TestKeySet) runner.Runner {
//	return decideRunner(spectestingutils.TestConsensusUnkownDutyTypeData, genesisspecqbft.FirstHeight, keySet)
//}
//
//var decideRunner = func(consensusInput *genesisspectypes.ConsensusData, height genesisspecqbft.Height, keySet *spectestingutils.TestKeySet) runner.Runner {
//	v := BaseValidator(keySet)
//	consensusDataByts, _ := consensusInput.Encode()
//	msgs := DecidingMsgsForHeight(consensusDataByts, []byte{1, 2, 3, 4}, height, keySet)
//
//	if err := v.DutyRunners[genesisspectypes.BNRoleAttester].StartNewDuty(consensusInput.Duty); err != nil {
//		panic(err.Error())
//	}
//	for _, msg := range msgs {
//		ssvMsg := SSVMsgAttester(msg, nil)
//		if err := v.ProcessMessage(ssvMsg); err != nil {
//			panic(err.Error())
//		}
//	}
//
//	return v.DutyRunners[genesisspectypes.BNRoleAttester]
//}
//
//var DecidingMsgsForHeight = func(consensusData, msgIdentifier []byte, height genesisspecqbft.Height, keySet *spectestingutils.TestKeySet) []*genesisspecqbft.SignedMessage {
//	msgs := make([]*genesisspecqbft.SignedMessage, 0)
//	for h := genesisspecqbft.FirstHeight; h <= height; h++ {
//		msgs = append(msgs, spectestingutils.SignQBFTMsg(keySet.Shares[1], 1, &genesisspecqbft.Message{
//			MsgType:    genesisspecqbft.ProposalMsgType,
//			Height:     h,
//			Round:      genesisspecqbft.FirstRound,
//			Identifier: msgIdentifier,
//			Data:       spectestingutils.ProposalDataBytes(consensusData, nil, nil),
//		}))
//
//		// prepare
//		for i := uint64(1); i <= keySet.Threshold; i++ {
//			msgs = append(msgs, spectestingutils.SignQBFTMsg(keySet.Shares[genesisspectypes.OperatorID(i)], genesisspectypes.OperatorID(i), &genesisspecqbft.Message{
//				MsgType:    genesisspecqbft.PrepareMsgType,
//				Height:     h,
//				Round:      genesisspecqbft.FirstRound,
//				Identifier: msgIdentifier,
//				Data:       spectestingutils.PrepareDataBytes(consensusData),
//			}))
//		}
//		// commit
//		for i := uint64(1); i <= keySet.Threshold; i++ {
//			msgs = append(msgs, spectestingutils.SignQBFTMsg(keySet.Shares[genesisspectypes.OperatorID(i)], genesisspectypes.OperatorID(i), &genesisspecqbft.Message{
//				MsgType:    genesisspecqbft.CommitMsgType,
//				Height:     h,
//				Round:      genesisspecqbft.FirstRound,
//				Identifier: msgIdentifier,
//				Data:       spectestingutils.CommitDataBytes(consensusData),
//			}))
//		}
//	}
//	return msgs
//}
