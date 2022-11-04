package utils

import (
	"context"
	"fmt"

	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv-spec/types/testingutils"

	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
	"github.com/bloxapp/ssv/protocol/v2/ssv/validator"
)

var BaseValidator = func(keySet *testingutils.TestKeySet) *validator.Validator {
	ssvShare, err := validator.ToSSVShare(testingutils.TestingShare(keySet))
	if err != nil {
		panic(fmt.Errorf("failed to convert spec share to ssv share - %s", err))
	}
	ssvMetadata, err := validator.ToSSVMetadata(testingutils.TestingShare(keySet))
	if err != nil {
		panic(fmt.Errorf("failed to convert spec share to ssv share - %s", err))
	}
	return validator.NewValidator(
		context.TODO(),
		validator.Options{
			Network:  testingutils.NewTestingNetwork(),
			Beacon:   testingutils.NewTestingBeaconNode(),
			Storage:  testingutils.NewTestingStorage(),
			Share:    ssvShare,
			Metadata: ssvMetadata,
			Signer:   testingutils.NewTestingKeyManager(),
			DutyRunners: map[types.BeaconRole]runner.Runner{
				types.BNRoleAttester:                  AttesterRunner(keySet),
				types.BNRoleProposer:                  ProposerRunner(keySet),
				types.BNRoleAggregator:                AggregatorRunner(keySet),
				types.BNRoleSyncCommittee:             SyncCommitteeRunner(keySet),
				types.BNRoleSyncCommitteeContribution: SyncCommitteeContributionRunner(keySet),
			},
		},
	)
}
