package scenarios

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"

	protocolstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
)

// Regular integration test.
// TODO: consider accepting scenario context - initialize if not passed - for scenario with multiple nodes on same network
func Regular() *IntegrationTest {
	roles := []spectypes.BeaconRole{
		spectypes.BNRoleAttester,
	}

	// TODO: use multiple roles
	identifier := spectypes.NewMsgID(spectestingutils.Testing4SharesSet().ValidatorPK.Serialize(), spectypes.BNRoleAttester)

	return &IntegrationTest{
		Name:             "regular",
		InitialInstances: nil,
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
						ID:    identifier[:],
					},
					// DecidedMessage: &specqbft.SignedMessage{
					// 	Signature: nil,
					// 	Signers:   nil,  // TODO: check in first implementation before checking decided message
					// 	Message:   &specqbft.Message{
					// 		MsgType:    specqbft.CommitMsgType, // TODO: check in first implementation before checking decided message
					// 		Height:     specqbft.FirstHeight, // TODO: check in first implementation before checking decided message
					// 		Round:      specqbft.FirstRound, // TODO: check in first implementation before checking decided message
					// 		Identifier: nil,
					// 		Data:       nil,
					// 	},
					// },
					// TODO: check decided messages
					// DecidedMessage: spectestingutils.MultiSignQBFTMsg([]*bls.SecretKey{spectestingutils.Testing4SharesSet().Shares[1], spectestingutils.Testing4SharesSet().Shares[2]}, []spectypes.OperatorID{1, 2}, &specqbft.Message{
					// 	MsgType:    specqbft.PrepareMsgType,
					// 	Height:     specqbft.FirstHeight,
					// 	Round:      specqbft.FirstRound,
					// 	Identifier: []byte{1, 2, 3, 4},
					// 	Data:       spectestingutils.PrepareDataBytes([]byte{1, 2, 3, 4}),
					// }),
				},
			},
			2: {
				{
					State: &specqbft.State{
						Share: testingShare(spectestingutils.Testing4SharesSet(), 2),
						ID:    identifier[:],
					},
				},
			},
			3: {
				{
					State: &specqbft.State{
						Share: testingShare(spectestingutils.Testing4SharesSet(), 3),
						ID:    identifier[:],
					},
				},
			},
			4: {
				{
					State: &specqbft.State{
						Share: testingShare(spectestingutils.Testing4SharesSet(), 4),
						ID:    identifier[:],
					},
				},
			},
		},
		ExpectedErrors: nil, // TODO: if it's nil, don't check errors
		OutputMessages: nil,
	}
}
