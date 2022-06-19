package synccommitteecontribution

import (
	"github.com/bloxapp/ssv/spec/qbft"
	"github.com/bloxapp/ssv/spec/ssv/spectest/tests"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/bloxapp/ssv/spec/types/testingutils"
)

// HappyFlow tests a full valcheck + post valcheck + duty sig reconstruction flow
func HappyFlow() *tests.SpecTest {
	ks := testingutils.Testing4SharesSet()
	dr := testingutils.SyncCommitteeContributionRunner(ks)

	msgs := []*types.SSVMessage{
		testingutils.SSVMsgSyncCommitteeContribution(nil, testingutils.PreConsensusContributionProofMsg(ks.Shares[1], 1)),
		testingutils.SSVMsgSyncCommitteeContribution(nil, testingutils.PreConsensusContributionProofMsg(ks.Shares[2], 2)),
		testingutils.SSVMsgSyncCommitteeContribution(nil, testingutils.PreConsensusContributionProofMsg(ks.Shares[3], 3)),

		testingutils.SSVMsgSyncCommitteeContribution(testingutils.SignQBFTMsg(ks.Shares[1], 1, &qbft.Message{
			MsgType:    qbft.ProposalMsgType,
			Height:     qbft.FirstHeight,
			Round:      qbft.FirstRound,
			Identifier: testingutils.SyncCommitteeContributionMsgID,
			Data:       testingutils.ProposalDataBytes(testingutils.TestSyncCommitteeContributionConsensusDataByts, nil, nil),
		}), nil),

		testingutils.SSVMsgSyncCommitteeContribution(testingutils.SignQBFTMsg(ks.Shares[1], 1, &qbft.Message{
			MsgType:    qbft.PrepareMsgType,
			Height:     qbft.FirstHeight,
			Round:      qbft.FirstRound,
			Identifier: testingutils.SyncCommitteeContributionMsgID,
			Data:       testingutils.PrepareDataBytes(testingutils.TestSyncCommitteeContributionConsensusDataByts),
		}), nil),
		testingutils.SSVMsgSyncCommitteeContribution(testingutils.SignQBFTMsg(ks.Shares[2], 2, &qbft.Message{
			MsgType:    qbft.PrepareMsgType,
			Height:     qbft.FirstHeight,
			Round:      qbft.FirstRound,
			Identifier: testingutils.SyncCommitteeContributionMsgID,
			Data:       testingutils.PrepareDataBytes(testingutils.TestSyncCommitteeContributionConsensusDataByts),
		}), nil),
		testingutils.SSVMsgSyncCommitteeContribution(testingutils.SignQBFTMsg(ks.Shares[3], 3, &qbft.Message{
			MsgType:    qbft.PrepareMsgType,
			Height:     qbft.FirstHeight,
			Round:      qbft.FirstRound,
			Identifier: testingutils.SyncCommitteeContributionMsgID,
			Data:       testingutils.PrepareDataBytes(testingutils.TestSyncCommitteeContributionConsensusDataByts),
		}), nil),

		testingutils.SSVMsgSyncCommitteeContribution(testingutils.SignQBFTMsg(ks.Shares[1], 1, &qbft.Message{
			MsgType:    qbft.CommitMsgType,
			Height:     qbft.FirstHeight,
			Round:      qbft.FirstRound,
			Identifier: testingutils.SyncCommitteeContributionMsgID,
			Data:       testingutils.CommitDataBytes(testingutils.TestSyncCommitteeContributionConsensusDataByts),
		}), nil),
		testingutils.SSVMsgSyncCommitteeContribution(testingutils.SignQBFTMsg(ks.Shares[2], 2, &qbft.Message{
			MsgType:    qbft.CommitMsgType,
			Height:     qbft.FirstHeight,
			Round:      qbft.FirstRound,
			Identifier: testingutils.SyncCommitteeContributionMsgID,
			Data:       testingutils.CommitDataBytes(testingutils.TestSyncCommitteeContributionConsensusDataByts),
		}), nil),
		testingutils.SSVMsgSyncCommitteeContribution(testingutils.SignQBFTMsg(ks.Shares[3], 3, &qbft.Message{
			MsgType:    qbft.CommitMsgType,
			Height:     qbft.FirstHeight,
			Round:      qbft.FirstRound,
			Identifier: testingutils.SyncCommitteeContributionMsgID,
			Data:       testingutils.CommitDataBytes(testingutils.TestSyncCommitteeContributionConsensusDataByts),
		}), nil),

		testingutils.SSVMsgSyncCommitteeContribution(nil, testingutils.PostConsensusSyncCommitteeContributionMsg(ks.Shares[1], 1, ks)),
		testingutils.SSVMsgSyncCommitteeContribution(nil, testingutils.PostConsensusSyncCommitteeContributionMsg(ks.Shares[2], 2, ks)),
		testingutils.SSVMsgSyncCommitteeContribution(nil, testingutils.PostConsensusSyncCommitteeContributionMsg(ks.Shares[3], 3, ks)),
	}

	return &tests.SpecTest{
		Name:                    "sync committee happy flow",
		Runner:                  dr,
		Duty:                    testingutils.TestingSyncCommitteeContributionDuty,
		Messages:                msgs,
		PostDutyRunnerStateRoot: "8e691db3678c651f788eb32c7e71879e74b09550a6b13995863ec3d84313ce64",
	}
}
