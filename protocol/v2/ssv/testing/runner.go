package testing

import (
	"bytes"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/doppelganger"
	"github.com/ssvlabs/ssv/integration/qbft/tests"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/testing"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

var TestingHighestDecidedSlot = phase0.Slot(0)

var CommitteeRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	return baseRunner(logger, spectypes.RoleCommittee, keySet)
}

var CommitteeRunnerWithShareMap = func(logger *zap.Logger, shareMap map[phase0.ValidatorIndex]*spectypes.Share) runner.Runner {
	return baseRunnerWithShareMap(logger, spectypes.RoleCommittee, shareMap)
}

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

var baseRunner = func(logger *zap.Logger, role spectypes.RunnerRole, keySet *spectestingutils.TestKeySet) runner.Runner {
	runner, err := ConstructBaseRunner(logger, role, keySet)
	if err != nil {
		panic(err)
	}
	return runner
}

var ConstructBaseRunner = func(
	logger *zap.Logger,
	role spectypes.RunnerRole,
	keySet *spectestingutils.TestKeySet,
) (runner.Runner, error) {
	share := spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex)
	identifier := spectypes.NewMsgID(spectypes.JatoTestnet, spectestingutils.TestingValidatorPubKey[:], role)
	net := spectestingutils.NewTestingNetwork(1, keySet.OperatorKeys[1])
	km := ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager())
	operator := spectestingutils.TestingCommitteeMember(keySet)
	opSigner := spectestingutils.NewOperatorSigner(keySet, 1)
	dgHandler := doppelganger.NoOpHandler{}

	var valCheck specqbft.ProposedValueCheckF
	switch role {
	case spectypes.RoleCommittee:
		valCheck = ssv.BeaconVoteValueCheckF(km, spectestingutils.TestingDutySlot,
			[]phase0.BLSPubKey{phase0.BLSPubKey(share.SharePubKey)}, spectestingutils.TestingDutyEpoch)
	case spectypes.RoleProposer:
		valCheck = ssv.ProposerValueCheckF(km, spectypes.BeaconTestNetwork,
			(spectypes.ValidatorPK)(spectestingutils.TestingValidatorPubKey), spectestingutils.TestingValidatorIndex, phase0.BLSPubKey(share.SharePubKey))
	case spectypes.RoleAggregator:
		valCheck = ssv.AggregatorValueCheckF(km, spectypes.BeaconTestNetwork,
			(spectypes.ValidatorPK)(spectestingutils.TestingValidatorPubKey), spectestingutils.TestingValidatorIndex)
	case spectypes.RoleSyncCommitteeContribution:
		valCheck = ssv.SyncCommitteeContributionValueCheckF(km, spectypes.BeaconTestNetwork,
			(spectypes.ValidatorPK)(spectestingutils.TestingValidatorPubKey), spectestingutils.TestingValidatorIndex)
	default:
		valCheck = nil
	}

	config := testing.TestingConfig(logger, keySet)
	config.ValueCheckF = valCheck
	config.ProposerF = func(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
		return 1
	}
	config.Network = net
	config.BeaconSigner = km

	contr := testing.NewTestingQBFTController(
		spectestingutils.Testing4SharesSet(),
		identifier[:],
		operator,
		config,
		false,
	)

	shareMap := make(map[phase0.ValidatorIndex]*spectypes.Share)
	shareMap[share.ValidatorIndex] = share
	dutyGuard := validator.NewCommitteeDutyGuard()

	var r runner.Runner
	var err error

	switch role {
	case spectypes.RoleCommittee:
		r, err = runner.NewCommitteeRunner(
			networkconfig.TestNetwork,
			shareMap,
			contr,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
			valCheck,
			dutyGuard,
			dgHandler,
		)
	case spectypes.RoleAggregator:
		r, err = runner.NewAggregatorRunner(
			networkconfig.TestNetwork.DomainType,
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
		r, err = runner.NewProposerRunner(
			networkconfig.TestNetwork.DomainType,
			spectypes.BeaconTestNetwork,
			shareMap,
			contr,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
			dgHandler,
			valCheck,
			TestingHighestDecidedSlot,
			[]byte("graffiti"),
		)
	case spectypes.RoleSyncCommitteeContribution:
		r, err = runner.NewSyncCommitteeAggregatorRunner(
			networkconfig.TestNetwork.DomainType,
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
		r, err = runner.NewValidatorRegistrationRunner(
			networkconfig.TestNetwork.DomainType,
			spectypes.BeaconTestNetwork,
			shareMap,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
			spectypes.DefaultGasLimit,
		)
	case spectypes.RoleVoluntaryExit:
		r, err = runner.NewVoluntaryExitRunner(
			networkconfig.TestNetwork.DomainType,
			spectypes.BeaconTestNetwork,
			shareMap,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
		)
	case spectestingutils.UnknownDutyType:
		r, err = runner.NewCommitteeRunner(
			networkconfig.TestNetwork,
			shareMap,
			contr,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
			valCheck,
			dutyGuard,
			dgHandler,
		)
		r.(*runner.CommitteeRunner).BaseRunner.RunnerRoleType = spectestingutils.UnknownDutyType
	default:
		return nil, errors.New("unknown role type")
	}
	return r, err
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

var baseRunnerWithShareMap = func(logger *zap.Logger, role spectypes.RunnerRole, shareMap map[phase0.ValidatorIndex]*spectypes.Share) runner.Runner {
	runner, err := ConstructBaseRunnerWithShareMap(logger, role, shareMap)
	if err != nil {
		panic(err)
	}
	return runner
}

var ConstructBaseRunnerWithShareMap = func(
	logger *zap.Logger,
	role spectypes.RunnerRole,
	shareMap map[phase0.ValidatorIndex]*spectypes.Share,
) (runner.Runner, error) {

	var identifier spectypes.MessageID
	var net *spectestingutils.TestingNetwork
	var opSigner *spectypes.OperatorSigner
	var valCheck specqbft.ProposedValueCheckF
	var contr *controller.Controller

	km := ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager())
	dutyGuard := validator.NewCommitteeDutyGuard()
	dgHandler := doppelganger.NoOpHandler{}

	if len(shareMap) > 0 {
		var keySetInstance *spectestingutils.TestKeySet
		var shareInstance *spectypes.Share
		for _, share := range shareMap {
			keySetInstance = spectestingutils.KeySetForShare(share)
			shareInstance = spectestingutils.TestingShare(keySetInstance, share.ValidatorIndex)
			break
		}

		sharePubKeys := make([]phase0.BLSPubKey, 0)
		for _, share := range shareMap {
			sharePubKeys = append(sharePubKeys, phase0.BLSPubKey(share.SharePubKey))
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
		identifier = spectypes.NewMsgID(spectestingutils.TestingSSVDomainType, ownerID, role)

		net = spectestingutils.NewTestingNetwork(1, keySetInstance.OperatorKeys[1])

		km = ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager())
		committeeMember := spectestingutils.TestingCommitteeMember(keySetInstance)
		opSigner = spectestingutils.NewOperatorSigner(keySetInstance, committeeMember.OperatorID)

		switch role {
		case spectypes.RoleCommittee:
			valCheck = ssv.BeaconVoteValueCheckF(km, spectestingutils.TestingDutySlot,
				sharePubKeys, spectestingutils.TestingDutyEpoch)
		case spectypes.RoleProposer:
			valCheck = ssv.ProposerValueCheckF(km, spectypes.BeaconTestNetwork,
				shareInstance.ValidatorPubKey, shareInstance.ValidatorIndex, phase0.BLSPubKey(shareInstance.SharePubKey))
		case spectypes.RoleAggregator:
			valCheck = ssv.AggregatorValueCheckF(km, spectypes.BeaconTestNetwork,
				shareInstance.ValidatorPubKey, shareInstance.ValidatorIndex)
		case spectypes.RoleSyncCommitteeContribution:
			valCheck = ssv.SyncCommitteeContributionValueCheckF(km, spectypes.BeaconTestNetwork,
				shareInstance.ValidatorPubKey, shareInstance.ValidatorIndex)
		default:
			valCheck = nil
		}

		config := testing.TestingConfig(logger, keySetInstance)
		config.ValueCheckF = valCheck
		config.ProposerF = func(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
			return 1
		}
		config.Network = net

		contr = testing.NewTestingQBFTController(
			spectestingutils.Testing4SharesSet(),
			identifier[:],
			committeeMember,
			config,
			false,
		)
	}

	var r runner.Runner
	var err error
	switch role {
	case spectypes.RoleCommittee:
		r, err = runner.NewCommitteeRunner(
			networkconfig.TestNetwork,
			shareMap,
			contr,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
			valCheck,
			dutyGuard,
			dgHandler,
		)
	case spectypes.RoleAggregator:
		r, err = runner.NewAggregatorRunner(
			networkconfig.TestNetwork.DomainType,
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
		r, err = runner.NewProposerRunner(
			networkconfig.TestNetwork.DomainType,
			spectypes.BeaconTestNetwork,
			shareMap,
			contr,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
			dgHandler,
			valCheck,
			TestingHighestDecidedSlot,
			[]byte("graffiti"),
		)
	case spectypes.RoleSyncCommitteeContribution:
		r, err = runner.NewSyncCommitteeAggregatorRunner(
			networkconfig.TestNetwork.DomainType,
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
		r, err = runner.NewValidatorRegistrationRunner(
			networkconfig.TestNetwork.DomainType,
			spectypes.BeaconTestNetwork,
			shareMap,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
			spectypes.DefaultGasLimit,
		)
	case spectypes.RoleVoluntaryExit:
		r, err = runner.NewVoluntaryExitRunner(
			networkconfig.TestNetwork.DomainType,
			spectypes.BeaconTestNetwork,
			shareMap,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
		)
	case spectestingutils.UnknownDutyType:
		r, err = runner.NewCommitteeRunner(
			networkconfig.TestNetwork,
			shareMap,
			contr,
			tests.NewTestingBeaconNodeWrapped(),
			net,
			km,
			opSigner,
			valCheck,
			dutyGuard,
			dgHandler,
		)
		if r != nil {
			r.(*runner.CommitteeRunner).BaseRunner.RunnerRoleType = spectestingutils.UnknownDutyType
		}
	default:
		return nil, errors.New("unknown role type")
	}
	return r, err
}
