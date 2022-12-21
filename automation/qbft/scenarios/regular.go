package scenarios

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"
	"go.uber.org/zap"

	protocolstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
)

// Regular integration test.
// TODO: avoid passing logger,
// consider accepting scenario context - initialize if not passed - for scenario with multiple nodes on same network
func Regular(logger *zap.Logger) *IntegrationTest {
	roles := []spectypes.BeaconRole{
		spectypes.BNRoleAttester,
	}

	return &IntegrationTest{
		Name:   "regular",
		Logger: logger,
		InitialInstances: map[spectypes.OperatorID][]*protocolstorage.StoredInstance{
			1: {
				{
					State: &specqbft.State{
						Share: testingShare(spectestingutils.Testing4SharesSet(), 1),
					},
				},
			},
			2: {
				{
					State: &specqbft.State{
						Share: testingShare(spectestingutils.Testing4SharesSet(), 2),
					},
				},
			},
			3: {
				{
					State: &specqbft.State{
						Share: testingShare(spectestingutils.Testing4SharesSet(), 3),
					},
				},
			},
			4: {
				{
					State: &specqbft.State{
						Share: testingShare(spectestingutils.Testing4SharesSet(), 4),
					},
				},
			},
		},
		Duties: map[spectypes.OperatorID][]*spectypes.Duty{
			1: createDuties(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 0, roles...),
			2: createDuties(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 1, roles...),
			3: createDuties(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 2, roles...),
			4: createDuties(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectestingutils.TestingDutySlot, 3, roles...),
		},
		ExpectedInstances: map[spectypes.OperatorID][]*protocolstorage.StoredInstance{
			1: {
				{
					State: &specqbft.State{
						Share: testingShare(spectestingutils.Testing4SharesSet(), 1),
					},
				},
			},
			2: {
				{
					State: &specqbft.State{
						Share: testingShare(spectestingutils.Testing4SharesSet(), 2),
					},
				},
			},
			3: {
				{
					State: &specqbft.State{
						Share: testingShare(spectestingutils.Testing4SharesSet(), 3),
					},
				},
			},
			4: {
				{
					State: &specqbft.State{
						Share: testingShare(spectestingutils.Testing4SharesSet(), 4),
					},
				},
			},
		},
		ExpectedErrors: map[spectypes.OperatorID]error{
			1: nil,
			2: nil,
			3: nil,
			4: nil,
		},
		OutputMessages: nil,
	}
}
