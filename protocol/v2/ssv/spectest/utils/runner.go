package utils

import (
	"github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/ssv"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner"
)

var AttesterRunner = func(keySet *testingutils.TestKeySet) runner.Runner {
	return baseRunner(types.BNRoleAttester, ssv.AttesterValueCheckF(testingutils.NewTestingKeyManager(), types.NowTestNetwork, testingutils.TestingValidatorPubKey[:], testingutils.TestingValidatorIndex), keySet)
}

var AttesterRunner7Operators = func(keySet *testingutils.TestKeySet) runner.Runner {
	return baseRunner(types.BNRoleAttester, ssv.AttesterValueCheckF(testingutils.NewTestingKeyManager(), types.NowTestNetwork, testingutils.TestingValidatorPubKey[:], testingutils.TestingValidatorIndex), keySet)
}

var ProposerRunner = func(keySet *testingutils.TestKeySet) runner.Runner {
	return baseRunner(types.BNRoleProposer, ssv.ProposerValueCheckF(testingutils.NewTestingKeyManager(), types.NowTestNetwork, testingutils.TestingValidatorPubKey[:], testingutils.TestingValidatorIndex), keySet)
}

var AggregatorRunner = func(keySet *testingutils.TestKeySet) runner.Runner {
	return baseRunner(types.BNRoleAggregator, ssv.AggregatorValueCheckF(testingutils.NewTestingKeyManager(), types.NowTestNetwork, testingutils.TestingValidatorPubKey[:], testingutils.TestingValidatorIndex), keySet)
}

var SyncCommitteeRunner = func(keySet *testingutils.TestKeySet) runner.Runner {
	return baseRunner(types.BNRoleSyncCommittee, ssv.SyncCommitteeValueCheckF(testingutils.NewTestingKeyManager(), types.NowTestNetwork, testingutils.TestingValidatorPubKey[:], testingutils.TestingValidatorIndex), keySet)
}

var SyncCommitteeContributionRunner = func(keySet *testingutils.TestKeySet) runner.Runner {
	return baseRunner(types.BNRoleSyncCommitteeContribution, ssv.SyncCommitteeContributionValueCheckF(testingutils.NewTestingKeyManager(), types.NowTestNetwork, testingutils.TestingValidatorPubKey[:], testingutils.TestingValidatorIndex), keySet)
}

var UnknownDutyTypeRunner = func(keySet *testingutils.TestKeySet) runner.Runner {
	return baseRunner(testingutils.UnknownDutyType, testingutils.UnknownDutyValueCheck(), keySet)
}

var baseRunner = func(role types.BeaconRole, valCheck qbft.ProposedValueCheckF, keySet *testingutils.TestKeySet) runner.Runner {
	share := testingutils.TestingShare(keySet)
	identifier := types.NewMsgID(testingutils.TestingValidatorPubKey[:], role)
	net := testingutils.NewTestingNetwork()
	km := testingutils.NewTestingKeyManager()

	config := testingutils.TestingConfig(keySet)
	config.ValueCheckF = valCheck
	config.ProposerF = func(state *qbft.State, round qbft.Round) types.OperatorID {
		return 1
	}
	config.Network = net
	config.Signer = km

	contr := controller.NewController(
		identifier[:],
		share,
		types.PrimusTestnet,
		config,
	)

	switch role {
	case types.BNRoleAttester:
		return runner.NewAttesterRunnner(
			types.NowTestNetwork,
			share,
			contr,
			testingutils.NewTestingBeaconNode(),
			net,
			km,
			valCheck,
		)
	case types.BNRoleAggregator:
		return runner.NewAggregatorRunner(
			types.NowTestNetwork,
			share,
			contr,
			testingutils.NewTestingBeaconNode(),
			net,
			km,
			valCheck,
		)
	case types.BNRoleProposer:
		return runner.NewProposerRunner(
			types.NowTestNetwork,
			share,
			contr,
			testingutils.NewTestingBeaconNode(),
			net,
			km,
			valCheck,
		)
	case types.BNRoleSyncCommittee:
		return runner.NewSyncCommitteeRunner(
			types.NowTestNetwork,
			share,
			contr,
			testingutils.NewTestingBeaconNode(),
			net,
			km,
			valCheck,
		)
	case types.BNRoleSyncCommitteeContribution:
		return runner.NewSyncCommitteeAggregatorRunner(
			types.NowTestNetwork,
			share,
			contr,
			testingutils.NewTestingBeaconNode(),
			net,
			km,
			valCheck,
		)
	case testingutils.UnknownDutyType:
		ret := runner.NewAttesterRunnner(
			types.NowTestNetwork,
			share,
			contr,
			testingutils.NewTestingBeaconNode(),
			net,
			km,
			valCheck,
		)
		ret.(*runner.AttesterRunner).BaseRunner.BeaconRoleType = testingutils.UnknownDutyType
		return ret
	default:
		panic("unknown role type")
	}
}

var DecidedRunner = func(keySet *testingutils.TestKeySet) runner.Runner {
	return decideRunner(testingutils.TestAttesterConsensusData, qbft.FirstHeight, keySet)
}

var DecidedRunnerWithHeight = func(height qbft.Height, keySet *testingutils.TestKeySet) runner.Runner {
	return decideRunner(testingutils.TestAttesterConsensusData, height, keySet)
}

var DecidedRunnerUnknownDutyType = func(keySet *testingutils.TestKeySet) runner.Runner {
	return decideRunner(testingutils.TestConsensusUnkownDutyTypeData, qbft.FirstHeight, keySet)
}

var decideRunner = func(consensusInput *types.ConsensusData, height qbft.Height, keySet *testingutils.TestKeySet) runner.Runner {
	v := BaseValidator(keySet)
	consensusDataByts, _ := consensusInput.Encode()
	msgs := DecidingMsgsForHeight(consensusDataByts, []byte{1, 2, 3, 4}, height, keySet)

	if err := v.DutyRunners[types.BNRoleAttester].StartNewDuty(consensusInput.Duty); err != nil {
		panic(err.Error())
	}
	for _, msg := range msgs {
		ssvMsg := SSVMsgAttester(msg, nil)
		if err := v.ProcessMessage(ssvMsg); err != nil {
			panic(err.Error())
		}
	}

	return v.DutyRunners[types.BNRoleAttester]
}

var SSVDecidingMsgs = func(consensusData []byte, ks *testingutils.TestKeySet, role types.BeaconRole) []*types.SSVMessage {
	id := types.NewMsgID(testingutils.TestingValidatorPubKey[:], role)

	ssvMsgF := func(qbftMsg *qbft.SignedMessage, partialSigMsg *ssv.SignedPartialSignatureMessage) *types.SSVMessage {
		var byts []byte
		var msgType types.MsgType
		if partialSigMsg != nil {
			msgType = types.SSVPartialSignatureMsgType
			byts, _ = partialSigMsg.Encode()
		} else {
			msgType = types.SSVConsensusMsgType
			byts, _ = qbftMsg.Encode()
		}

		return &types.SSVMessage{
			MsgType: msgType,
			MsgID:   id,
			Data:    byts,
		}
	}

	// pre consensus msgs
	base := make([]*types.SSVMessage, 0)
	if role == types.BNRoleProposer {
		for i := uint64(1); i <= ks.Threshold; i++ {
			base = append(base, ssvMsgF(nil, PreConsensusRandaoMsg(ks.Shares[types.OperatorID(i)], types.OperatorID(i))))
		}
	}
	if role == types.BNRoleAggregator {
		for i := uint64(1); i <= ks.Threshold; i++ {
			base = append(base, ssvMsgF(nil, PreConsensusSelectionProofMsg(ks.Shares[types.OperatorID(i)], ks.Shares[types.OperatorID(i)], types.OperatorID(i), types.OperatorID(i))))
		}
	}
	if role == types.BNRoleSyncCommitteeContribution {
		for i := uint64(1); i <= ks.Threshold; i++ {
			base = append(base, ssvMsgF(nil, PreConsensusContributionProofMsg(ks.Shares[types.OperatorID(i)], ks.Shares[types.OperatorID(i)], types.OperatorID(i), types.OperatorID(i))))
		}
	}

	qbftMsgs := DecidingMsgsForHeight(consensusData, id[:], qbft.FirstHeight, ks)
	for _, msg := range qbftMsgs {
		base = append(base, ssvMsgF(msg, nil))
	}
	return base
}

var DecidingMsgsForHeight = func(consensusData, msgIdentifier []byte, height qbft.Height, keySet *testingutils.TestKeySet) []*qbft.SignedMessage {
	msgs := make([]*qbft.SignedMessage, 0)
	for h := qbft.FirstHeight; h <= height; h++ {
		msgs = append(msgs, testingutils.SignQBFTMsg(keySet.Shares[1], 1, &qbft.Message{
			MsgType:    qbft.ProposalMsgType,
			Height:     h,
			Round:      qbft.FirstRound,
			Identifier: msgIdentifier,
			Data:       testingutils.ProposalDataBytes(consensusData, nil, nil),
		}))

		// prepare
		for i := uint64(1); i <= keySet.Threshold; i++ {
			msgs = append(msgs, testingutils.SignQBFTMsg(keySet.Shares[types.OperatorID(i)], types.OperatorID(i), &qbft.Message{
				MsgType:    qbft.PrepareMsgType,
				Height:     h,
				Round:      qbft.FirstRound,
				Identifier: msgIdentifier,
				Data:       testingutils.PrepareDataBytes(consensusData),
			}))
		}
		// commit
		for i := uint64(1); i <= keySet.Threshold; i++ {
			msgs = append(msgs, testingutils.SignQBFTMsg(keySet.Shares[types.OperatorID(i)], types.OperatorID(i), &qbft.Message{
				MsgType:    qbft.CommitMsgType,
				Height:     h,
				Round:      qbft.FirstRound,
				Identifier: msgIdentifier,
				Data:       testingutils.CommitDataBytes(consensusData),
			}))
		}
	}
	return msgs
}
