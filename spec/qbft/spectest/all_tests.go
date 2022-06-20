package spectest

import (
	"github.com/bloxapp/ssv/spec/qbft/spectest/tests"
"
"github.com/bloxapp/ssv/spec/qbft/spectest/tests"/commit"
"github.com/bloxapp/ssv/spec/qbft/spectest/tests"/messages"
"github.com/bloxapp/ssv/spec/qbft/spectest/tests"/roundchange"
"testing"
)
type SpecTest interface {
	TestName() string
	Run(t *testing.T)
}

var AllTests = []SpecTest{
	messages.CommitDataEncoding(),
	messages.DecidedMsgEncoding(),
	messages.MsgNilIdentifier(),
	messages.MsgNonZeroIdentifier(),
	messages.MsgTypeUnknown(),
	messages.PrepareDataEncoding(),
	messages.ProposeDataEncoding(),
	messages.MsgDataNil(),
	messages.MsgDataNonZero(),
	messages.SignedMsgSigTooShort(),
	messages.SignedMsgSigTooLong(),
	messages.SignedMsgNoSigners(),
	messages.GetRoot(),
	messages.SignedMessageEncoding(),
	messages.CreateProposal(),
	messages.CreateProposalPreviouslyPrepared(),
	messages.CreateProposalNotPreviouslyPrepared(),
	messages.CreatePrepare(),
	messages.CreateCommit(),
	messages.CreateRoundChange(),
	messages.CreateRoundChangePreviouslyPrepared(),
	messages.RoundChangeDataEncoding(),

	tests.HappyFlow(),
	tests.SevenOperators(),
	tests.TenOperators(),
	tests.ThirteenOperators(),

	commit.CurrentRound(),
	commit.FutureRound(),
	commit.PastRound(),
	commit.DuplicateMsg(),
	commit.HappyFlow(),
	commit.InvalidCommitData(),
	commit.PostDecided(),
	commit.WrongData1(),
	commit.WrongData2(),
	commit.MultiSignerWithOverlap(),
	commit.MultiSignerNoOverlap(),
	commit.Decided(),
	commit.NoPrevAcceptedProposal(),
	commit.WrongHeight(),
	commit.ImparsableCommitData(),
	commit.WrongSignature(),

	roundchange.HappyFlow(),
	roundchange.PreviouslyPrepared(),
	roundchange.F1Speedup(),
}
