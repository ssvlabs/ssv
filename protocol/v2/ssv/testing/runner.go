package testing

import (
	"fmt"

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

var AggregatorCommitteeRunner = func(logger *zap.Logger, keySet *spectestingutils.TestKeySet) runner.Runner {
	return baseRunner(logger, spectypes.RoleAggregatorCommittee, keySet)
}

var AggregatorCommitteeRunnerWithShareMap = func(logger *zap.Logger, shareMap map[phase0.ValidatorIndex]*spectypes.Share) runner.Runner {
	return baseRunnerWithShareMap(logger, spectypes.RoleAggregatorCommittee, shareMap)
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
	ret := baseRunner(logger, ssvtypes.RoleValidatorRegistration, keySet)
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
	identifier := spectypes.NewValidatorMsgID(spectypes.JatoTestnet, spectypes.ValidatorPK(spectestingutils.TestingValidatorPubKey), role)
	net := protocoltesting.NewTestingNetwork(1, keySet.OperatorKeys[1])
	km := ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager())
	operator := spectestingutils.TestingCommitteeMember(keySet)
	opSigner := spectestingutils.NewOperatorSigner(keySet, 1)

	valCheck := valueChecker(role, km, spectypes.ValidatorPK(spectestingutils.TestingValidatorPubKey),
		spectestingutils.TestingValidatorIndex, phase0.BLSPubKey(share.SharePubKey))

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

	baseOpts := runner.BaseRunnerOptions{
		NetworkConfig:  networkconfig.TestNetwork,
		Share:          shareMap,
		Beacon:         protocoltesting.NewTestingBeaconNodeWrapped(),
		Network:        net,
		Signer:         km,
		OperatorSigner: opSigner,
	}
	return newRunner(role, baseOpts, contr, valCheck, []phase0.BLSPubKey{phase0.BLSPubKey(share.SharePubKey)})
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

		// Identifier: committee and aggregator-committee runners key by CommitteeID; all others by validator pubkey.
		switch role {
		case spectypes.RoleCommittee, spectypes.RoleAggregatorCommittee:
			ops := keySetInstance.Committee()
			committee := make([]uint64, 0, len(ops))
			for _, op := range ops {
				committee = append(committee, op.Signer)
			}
			committeeID := spectypes.GetCommitteeID(committee)
			identifier = spectypes.NewCommitteeMsgID(spectestingutils.TestingSSVDomainType, committeeID, role)
		default:
			identifier = spectypes.NewValidatorMsgID(spectestingutils.TestingSSVDomainType, spectypes.ValidatorPK(spectestingutils.TestingValidatorPubKey), role)
		}

		net = protocoltesting.NewTestingNetwork(1, keySetInstance.OperatorKeys[1])

		committeeMember := spectestingutils.TestingCommitteeMember(keySetInstance)
		opSigner = spectestingutils.NewOperatorSigner(keySetInstance, committeeMember.OperatorID)

		valCheck = valueChecker(role, km, shareInstance.ValidatorPubKey, shareInstance.ValidatorIndex,
			phase0.BLSPubKey(shareInstance.SharePubKey))

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

	baseOpts := runner.BaseRunnerOptions{
		NetworkConfig:  networkconfig.TestNetwork,
		Share:          shareMap,
		Beacon:         protocoltesting.NewTestingBeaconNodeWrapped(),
		Network:        net,
		Signer:         km,
		OperatorSigner: opSigner,
	}
	return newRunner(role, baseOpts, contr, valCheck, sharePubKeys)
}

// valueChecker returns the value checker of the runners that take one. The committee runners build their own.
func valueChecker(
	role spectypes.RunnerRole,
	signer ekm.BeaconSigner,
	validatorPK spectypes.ValidatorPK,
	validatorIndex phase0.ValidatorIndex,
	sharePubKey phase0.BLSPubKey,
) ssv.ValueChecker {
	beaconConfig := networkconfig.TestNetwork.Beacon
	switch role {
	case spectypes.RoleProposer:
		return ssv.NewProposerChecker(signer, beaconConfig, validatorPK, validatorIndex, sharePubKey, nil)
	case ssvtypes.RoleAggregator:
		return ssv.NewAggregatorChecker(beaconConfig, validatorPK, validatorIndex)
	case ssvtypes.RoleSyncCommitteeContribution:
		return ssv.NewSyncCommitteeContributionChecker(beaconConfig, validatorPK, validatorIndex)
	default:
		return nil
	}
}

// newRunner constructs the role's runner from the options both constructors build. The committee runners attest
// for attestingValidators.
func newRunner(
	role spectypes.RunnerRole,
	baseOpts runner.BaseRunnerOptions,
	contr *controller.Controller,
	valCheck ssv.ValueChecker,
	attestingValidators []phase0.BLSPubKey,
) (runner.Runner, error) {
	switch role {
	case spectypes.RoleCommittee, spectestingutils.UnknownDutyType:
		r, err := runner.NewCommitteeRunner(runner.CommitteeRunnerOptions{
			BaseRunnerOptions:   baseOpts,
			AttestingValidators: attestingValidators,
			QBFTController:      contr,
			DutyGuard:           validator.NewCommitteeDutyGuard(),
			DoppelgangerHandler: doppelganger.NoOpHandler{},
		})
		if err != nil {
			return nil, err
		}
		if role == spectestingutils.UnknownDutyType {
			r.(*runner.CommitteeRunner).RunnerRoleType = spectestingutils.UnknownDutyType
		}
		return r, nil
	case spectypes.RoleAggregatorCommittee:
		return runner.NewAggregatorCommitteeRunner(runner.AggregatorCommitteeRunnerOptions{
			BaseRunnerOptions: baseOpts,
			QBFTController:    contr,
		})
	case ssvtypes.RoleAggregator:
		r, err := runner.NewAggregatorRunner(runner.AggregatorRunnerOptions{
			BaseRunnerOptions:  baseOpts,
			QBFTController:     contr,
			ValCheck:           valCheck,
			HighestDecidedSlot: TestingHighestDecidedSlot,
		})
		if err != nil {
			return nil, err
		}
		r.(*runner.AggregatorRunner).IsAggregator = func(_ uint64, _ uint64, _ []byte) bool {
			return true
		}
		return r, nil
	case spectypes.RoleProposer:
		return runner.NewProposerRunner(runner.ProposerRunnerOptions{
			BaseRunnerOptions:   baseOpts,
			QBFTController:      contr,
			DoppelgangerHandler: doppelganger.NoOpHandler{},
			ValCheck:            valCheck,
			HighestDecidedSlot:  TestingHighestDecidedSlot,
			Graffiti:            []byte("graffiti"),
			ProposerDelay:       0,
		})
	case ssvtypes.RoleSyncCommitteeContribution:
		return runner.NewSyncCommitteeAggregatorRunner(runner.SyncCommitteeAggregatorRunnerOptions{
			BaseRunnerOptions:  baseOpts,
			QBFTController:     contr,
			ValCheck:           valCheck,
			HighestDecidedSlot: TestingHighestDecidedSlot,
		})
	case ssvtypes.RoleValidatorRegistration:
		return runner.NewValidatorRegistrationRunner(runner.ValidatorRegistrationRunnerOptions{
			BaseRunnerOptions:              baseOpts,
			ValidatorRegistrationSubmitter: mocks.NewValidatorRegistrationSubmitter(baseOpts.Beacon),
			FeeRecipientProvider:           &mocks.FeeRecipientProvider{},
			GasLimit:                       spectypes.DefaultGasLimit,
		})
	case spectypes.RoleVoluntaryExit:
		return runner.NewVoluntaryExitRunner(runner.VoluntaryExitRunnerOptions{
			BaseRunnerOptions: baseOpts,
		})
	case spectypes.RolePTCAttester:
		return runner.NewPTCAttesterRunner(runner.PTCAttesterRunnerOptions{
			BaseRunnerOptions: baseOpts,
		})
	case spectypes.RoleProposerPreferences:
		return runner.NewProposerPreferencesRunner(runner.ProposerPreferencesRunnerOptions{
			BaseRunnerOptions:    baseOpts,
			FeeRecipientProvider: &mocks.FeeRecipientProvider{},
			GasLimit:             spectypes.DefaultGasLimit,
		})
	default:
		return nil, fmt.Errorf("unknown role type: %s", role)
	}
}
