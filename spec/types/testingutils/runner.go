package testingutils

import (
	"github.com/bloxapp/ssv/spec/qbft"
	"github.com/bloxapp/ssv/spec/ssv"
	"github.com/bloxapp/ssv/spec/types"
)

var AttesterRunner = func(keySet *TestKeySet) *ssv.Runner {
	return baseRunner(types.BNRoleAttester, ssv.BeaconAttestationValueCheck(NewTestingKeyManager(), ssv.NowTestNetwork), keySet)
}

var AttesterRunner7Operators = func(keySet *TestKeySet) *ssv.Runner {
	return baseRunner(types.BNRoleAttester, ssv.BeaconAttestationValueCheck(NewTestingKeyManager(), ssv.NowTestNetwork), keySet)
}

var ProposerRunner = func(keySet *TestKeySet) *ssv.Runner {
	return baseRunner(types.BNRoleProposer, ssv.BeaconBlockValueCheck(NewTestingKeyManager(), ssv.NowTestNetwork), keySet)
}

var AggregatorRunner = func(keySet *TestKeySet) *ssv.Runner {
	return baseRunner(types.BNRoleAggregator, ssv.AggregatorValueCheck(NewTestingKeyManager(), ssv.NowTestNetwork), keySet)
}

var SyncCommitteeRunner = func(keySet *TestKeySet) *ssv.Runner {
	return baseRunner(types.BNRoleSyncCommittee, ssv.SyncCommitteeValueCheck(NewTestingKeyManager(), ssv.NowTestNetwork), keySet)
}

var SyncCommitteeContributionRunner = func(keySet *TestKeySet) *ssv.Runner {
	return baseRunner(types.BNRoleSyncCommitteeContribution, ssv.SyncCommitteeContributionValueCheck(NewTestingKeyManager(), ssv.NowTestNetwork), keySet)
}

var baseRunner = func(role types.BeaconRole, valCheck qbft.ProposedValueCheck, keySet *TestKeySet) *ssv.Runner {
	share := TestingShare(keySet)
	identifier := types.NewMsgID(TestingValidatorPubKey[:], role)

	return ssv.NewDutyRunner(
		role,
		ssv.NowTestNetwork,
		share,
		NewTestingQBFTController(identifier[:], share, valCheck),
		NewTestingStorage(),
		valCheck,
	)
}

var DecidedRunner = func(keySet *TestKeySet) *ssv.Runner {
	return decideRunner(TestAttesterConsensusDataByts, qbft.FirstHeight, keySet)
}

var DecidedRunnerWithHeight = func(height qbft.Height, keySet *TestKeySet) *ssv.Runner {
	return decideRunner(TestAttesterConsensusDataByts, height, keySet)
}

var DecidedRunnerUnknownDutyType = func(keySet *TestKeySet) *ssv.Runner {
	return decideRunner(TestConsensusUnkownDutyTypeDataByts, qbft.FirstHeight, keySet)
}

var decideRunner = func(consensusData []byte, height qbft.Height, keySet *TestKeySet) *ssv.Runner {
	v := BaseValidator(keySet)
	for h := qbft.Height(qbft.FirstHeight); h <= height; h++ {
		msgs := []*types.SSVMessage{
			SSVMsgAttester(SignQBFTMsg(keySet.Shares[1], 1, &qbft.Message{
				MsgType:    qbft.ProposalMsgType,
				Height:     h,
				Round:      qbft.FirstRound,
				Identifier: []byte{1, 2, 3, 4},
				Data:       ProposalDataBytes(consensusData, nil, nil),
			}), nil),
		}

		// prepare
		for i := uint64(1); i <= keySet.Threshold; i++ {
			msgs = append(msgs, SSVMsgAttester(SignQBFTMsg(keySet.Shares[types.OperatorID(i)], types.OperatorID(i), &qbft.Message{
				MsgType:    qbft.PrepareMsgType,
				Height:     h,
				Round:      qbft.FirstRound,
				Identifier: []byte{1, 2, 3, 4},
				Data:       PrepareDataBytes(consensusData),
			}), nil))
		}
		// commit
		for i := uint64(1); i <= keySet.Threshold; i++ {
			msgs = append(msgs, SSVMsgAttester(SignQBFTMsg(keySet.Shares[types.OperatorID(i)], types.OperatorID(i), &qbft.Message{
				MsgType:    qbft.CommitMsgType,
				Height:     h,
				Round:      qbft.FirstRound,
				Identifier: []byte{1, 2, 3, 4},
				Data:       CommitDataBytes(consensusData),
			}), nil))
		}

		if err := v.DutyRunners[types.BNRoleAttester].Decide(TestAttesterConsensusData); err != nil {
			panic(err.Error())
		}
		for _, msg := range msgs {
			if err := v.ProcessMessage(msg); err != nil {
				panic(err.Error())
			}
		}
	}

	return v.DutyRunners[types.BNRoleAttester]
}
