package testing

import (
	"bytes"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/doppelganger"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/testing/mocks"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
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
	return baseRunner(logger, ssvtypes.RoleAggregator, keySet)
}

var SyncCommitteeContributionRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	return baseRunner(logger, ssvtypes.RoleSyncCommitteeContribution, keySet)
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

var vote = &spectypes.BeaconVote{
	BlockRoot: spectestingutils.TestingAttestationData(spec.DataVersionPhase0).BeaconBlockRoot,
	Source:    spectestingutils.TestingAttestationData(spec.DataVersionPhase0).Source,
	Target:    spectestingutils.TestingAttestationData(spec.DataVersionPhase0).Target,
}

var ConstructBaseRunner = func(
	logger *zap.Logger,
	role spectypes.RunnerRole,
	keySet *spectestingutils.TestKeySet,
) (runner.Runner, error) {
	share := spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex)
	identifier := spectypes.NewMsgID(spectypes.JatoTestnet, spectestingutils.TestingValidatorPubKey[:], role)
	net := protocoltesting.NewTestingNetwork(1, keySet.OperatorKeys[1])
	km := ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager())
	operator := spectestingutils.TestingCommitteeMember(keySet)
	opSigner := spectestingutils.NewOperatorSigner(keySet, 1)
	dgHandler := doppelganger.NoOpHandler{}

	var valCheck ssv.ValueChecker
	switch role {
	case spectypes.RoleCommittee:
		valCheck = ssv.NewVoteChecker(km, spectestingutils.TestingDutySlot,
			[]phase0.BLSPubKey{phase0.BLSPubKey(share.SharePubKey)}, vote)
	case spectypes.RoleProposer:
		valCheck = ssv.NewProposerChecker(km, networkconfig.TestNetwork.Beacon,
			(spectypes.ValidatorPK)(spectestingutils.TestingValidatorPubKey), spectestingutils.TestingValidatorIndex,
			phase0.BLSPubKey(share.SharePubKey))
	case ssvtypes.RoleAggregator:
		valCheck = ssv.NewAggregatorChecker(networkconfig.TestNetwork.Beacon,
			(spectypes.ValidatorPK)(spectestingutils.TestingValidatorPubKey), spectestingutils.TestingValidatorIndex)
	case ssvtypes.RoleSyncCommitteeContribution:
		valCheck = ssv.NewSyncCommitteeContributionChecker(networkconfig.TestNetwork.Beacon,
			(spectypes.ValidatorPK)(spectestingutils.TestingValidatorPubKey), spectestingutils.TestingValidatorIndex)
	default:
		valCheck = nil
	}

	config := protocoltesting.TestingConfig(logger, keySet)
	config.ProposerF = func(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
		return 1
	}
	config.Network = net
	config.BeaconSigner = km

	contr := protocoltesting.NewTestingQBFTController(
		spectestingutils.Testing4SharesSet(),
		identifier[:],
		operator,
		config,
		false,
	)

	shareMap := make(map[phase0.ValidatorIndex]*spectypes.Share)
	shareMap[share.ValidatorIndex] = share
	dutyGuard := validator.NewCommitteeDutyGuard()

	beaconNode := protocoltesting.NewTestingBeaconNodeWrapped()
	baseOpts := runner.BaseRunnerOptions{
		NetworkConfig:  networkconfig.TestNetwork,
		Share:          shareMap,
		Beacon:         beaconNode,
		Network:        net,
		Signer:         km,
		OperatorSigner: opSigner,
	}

	var r runner.Runner
	var err error
	switch role {
	case spectypes.RoleCommittee:
		r, err = runner.NewCommitteeRunner(runner.CommitteeRunnerOptions{
			BaseRunnerOptions:   baseOpts,
			AttestingValidators: []phase0.BLSPubKey{phase0.BLSPubKey(share.SharePubKey)},
			QBFTController:      contr,
			DutyGuard:           dutyGuard,
			DoppelgangerHandler: dgHandler,
		})
	case ssvtypes.RoleAggregator:
		rnr, err := runner.NewAggregatorRunner(runner.AggregatorRunnerOptions{
			BaseRunnerOptions:  baseOpts,
			QBFTController:     contr,
			ValCheck:           valCheck,
			HighestDecidedSlot: TestingHighestDecidedSlot,
		})
		if err != nil {
			return nil, err
		}
		rnr.(*runner.AggregatorRunner).IsAggregator = func(_ uint64, _ uint64, _ []byte) bool {
			return true
		}
		r = rnr
	case spectypes.RoleProposer:
		r, err = runner.NewProposerRunner(runner.ProposerRunnerOptions{
			BaseRunnerOptions:   baseOpts,
			QBFTController:      contr,
			DoppelgangerHandler: dgHandler,
			ValCheck:            valCheck,
			HighestDecidedSlot:  TestingHighestDecidedSlot,
			Graffiti:            []byte("graffiti"),
			ProposerDelay:       0,
		})
	case ssvtypes.RoleSyncCommitteeContribution:
		r, err = runner.NewSyncCommitteeAggregatorRunner(runner.SyncCommitteeAggregatorRunnerOptions{
			BaseRunnerOptions:  baseOpts,
			QBFTController:     contr,
			ValCheck:           valCheck,
			HighestDecidedSlot: TestingHighestDecidedSlot,
		})
	case spectypes.RoleValidatorRegistration:
		mockFeeProvider := &mocks.FeeRecipientProvider{}
		r, err = runner.NewValidatorRegistrationRunner(runner.ValidatorRegistrationRunnerOptions{
			BaseRunnerOptions:              baseOpts,
			ValidatorRegistrationSubmitter: mocks.NewValidatorRegistrationSubmitter(beaconNode),
			FeeRecipientProvider:           mockFeeProvider,
			GasLimit:                       spectypes.DefaultGasLimit,
		})
	case spectypes.RoleVoluntaryExit:
		r, err = runner.NewVoluntaryExitRunner(runner.VoluntaryExitRunnerOptions{
			BaseRunnerOptions: baseOpts,
		})
	case spectestingutils.UnknownDutyType:
		r, err = runner.NewCommitteeRunner(runner.CommitteeRunnerOptions{
			BaseRunnerOptions:   baseOpts,
			AttestingValidators: []phase0.BLSPubKey{phase0.BLSPubKey(share.SharePubKey)},
			QBFTController:      contr,
			DutyGuard:           dutyGuard,
			DoppelgangerHandler: dgHandler,
		})
		r.(*runner.CommitteeRunner).RunnerRoleType = spectestingutils.UnknownDutyType
	default:
		return nil, fmt.Errorf("unknown role type: %s", role)
	}
	return r, err
}
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
	var net *protocoltesting.TestingNetwork
	var opSigner *spectypes.OperatorSigner
	var valCheck ssv.ValueChecker
	var contr *controller.Controller

	km := ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager())
	dutyGuard := validator.NewCommitteeDutyGuard()
	dgHandler := doppelganger.NoOpHandler{}

	sharePubKeys := make([]phase0.BLSPubKey, 0)
	if len(shareMap) > 0 {
		var keySetInstance *spectestingutils.TestKeySet
		var shareInstance *spectypes.Share
		for _, share := range shareMap {
			keySetInstance = spectestingutils.KeySetForShare(share)
			shareInstance = spectestingutils.TestingShare(keySetInstance, share.ValidatorIndex)
			break
		}

		for _, share := range shareMap {
			sharePubKeys = append(sharePubKeys, phase0.BLSPubKey(share.SharePubKey))
		}

		// Identifier
		var ownerID []byte
		if role == spectypes.RoleCommittee {
			ops := keySetInstance.Committee()
			committee := make([]uint64, 0, len(ops))
			for _, op := range ops {
				committee = append(committee, op.Signer)
			}
			committeeID := spectypes.GetCommitteeID(committee)
			ownerID = bytes.Clone(committeeID[:])
		} else {
			ownerID = spectestingutils.TestingValidatorPubKey[:]
		}
		identifier = spectypes.NewMsgID(spectestingutils.TestingSSVDomainType, ownerID, role)

		net = protocoltesting.NewTestingNetwork(1, keySetInstance.OperatorKeys[1])

		km = ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager())
		committeeMember := spectestingutils.TestingCommitteeMember(keySetInstance)
		opSigner = spectestingutils.NewOperatorSigner(keySetInstance, committeeMember.OperatorID)

		switch role {
		case spectypes.RoleCommittee:
			valCheck = ssv.NewVoteChecker(km, spectestingutils.TestingDutySlot,
				sharePubKeys, vote)
		case spectypes.RoleProposer:
			valCheck = ssv.NewProposerChecker(km, networkconfig.TestNetwork.Beacon,
				shareInstance.ValidatorPubKey, shareInstance.ValidatorIndex, phase0.BLSPubKey(shareInstance.SharePubKey))
		case ssvtypes.RoleAggregator:
			valCheck = ssv.NewAggregatorChecker(networkconfig.TestNetwork.Beacon,
				shareInstance.ValidatorPubKey, shareInstance.ValidatorIndex)
		case ssvtypes.RoleSyncCommitteeContribution:
			valCheck = ssv.NewSyncCommitteeContributionChecker(networkconfig.TestNetwork.Beacon,
				shareInstance.ValidatorPubKey, shareInstance.ValidatorIndex)
		default:
			valCheck = nil
		}

		config := protocoltesting.TestingConfig(logger, keySetInstance)
		config.ProposerF = func(state *specqbft.State, round specqbft.Round) spectypes.OperatorID {
			return 1
		}
		config.Network = net

		contr = protocoltesting.NewTestingQBFTController(
			spectestingutils.Testing4SharesSet(),
			identifier[:],
			committeeMember,
			config,
			false,
		)
	}

	beaconNode := protocoltesting.NewTestingBeaconNodeWrapped()
	baseOpts := runner.BaseRunnerOptions{
		NetworkConfig:  networkconfig.TestNetwork,
		Share:          shareMap,
		Beacon:         beaconNode,
		Network:        net,
		Signer:         km,
		OperatorSigner: opSigner,
	}

	var r runner.Runner
	var err error
	switch role {
	case spectypes.RoleCommittee:
		r, err = runner.NewCommitteeRunner(runner.CommitteeRunnerOptions{
			BaseRunnerOptions:   baseOpts,
			AttestingValidators: sharePubKeys,
			QBFTController:      contr,
			DutyGuard:           dutyGuard,
			DoppelgangerHandler: dgHandler,
		})
	case ssvtypes.RoleAggregator:
		rnr, err := runner.NewAggregatorRunner(runner.AggregatorRunnerOptions{
			BaseRunnerOptions:  baseOpts,
			QBFTController:     contr,
			ValCheck:           valCheck,
			HighestDecidedSlot: TestingHighestDecidedSlot,
		})
		if err != nil {
			return nil, err
		}
		rnr.(*runner.AggregatorRunner).IsAggregator = func(_ uint64, _ uint64, _ []byte) bool {
			return true
		}
		r = rnr
	case spectypes.RoleProposer:
		r, err = runner.NewProposerRunner(runner.ProposerRunnerOptions{
			BaseRunnerOptions:   baseOpts,
			QBFTController:      contr,
			DoppelgangerHandler: dgHandler,
			ValCheck:            valCheck,
			HighestDecidedSlot:  TestingHighestDecidedSlot,
			Graffiti:            []byte("graffiti"),
			ProposerDelay:       0,
		})
	case ssvtypes.RoleSyncCommitteeContribution:
		r, err = runner.NewSyncCommitteeAggregatorRunner(runner.SyncCommitteeAggregatorRunnerOptions{
			BaseRunnerOptions:  baseOpts,
			QBFTController:     contr,
			ValCheck:           valCheck,
			HighestDecidedSlot: TestingHighestDecidedSlot,
		})
	case spectypes.RoleValidatorRegistration:
		mockFeeProvider := &mocks.FeeRecipientProvider{}
		r, err = runner.NewValidatorRegistrationRunner(runner.ValidatorRegistrationRunnerOptions{
			BaseRunnerOptions:              baseOpts,
			ValidatorRegistrationSubmitter: mocks.NewValidatorRegistrationSubmitter(beaconNode),
			FeeRecipientProvider:           mockFeeProvider,
			GasLimit:                       spectypes.DefaultGasLimit,
		})
	case spectypes.RoleVoluntaryExit:
		r, err = runner.NewVoluntaryExitRunner(runner.VoluntaryExitRunnerOptions{
			BaseRunnerOptions: baseOpts,
		})
	case spectestingutils.UnknownDutyType:
		r, err = runner.NewCommitteeRunner(runner.CommitteeRunnerOptions{
			BaseRunnerOptions:   baseOpts,
			AttestingValidators: sharePubKeys,
			QBFTController:      contr,
			DutyGuard:           dutyGuard,
			DoppelgangerHandler: dgHandler,
		})
		if r != nil {
			r.(*runner.CommitteeRunner).RunnerRoleType = spectestingutils.UnknownDutyType
		}
	default:
		return nil, fmt.Errorf("unknown role type: %s", role)
	}
	return r, err
}
