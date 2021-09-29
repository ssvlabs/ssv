package tests

import (
	"github.com/bloxapp/ssv/ibft/spectesting/tests/changeround"
	"github.com/bloxapp/ssv/ibft/spectesting/tests/commit"
	"github.com/bloxapp/ssv/ibft/spectesting/tests/common"
	"github.com/bloxapp/ssv/ibft/spectesting/tests/prepare"
	"github.com/bloxapp/ssv/ibft/spectesting/tests/preprepare"
	"github.com/stretchr/testify/require"
	"testing"
)

type SpecTest interface {
	Name() string
	// Prepare sets all testing fixtures and params before running the test
	Prepare(t *testing.T)
	// Run will execute the test.
	Run(t *testing.T)
}

var tests = []SpecTest{
	// pre-prepare
	&preprepare.NonJustifiedPrePrepapre1{},
	&preprepare.NonJustifiedPrePrepapre2{},
	&preprepare.NonJustifiedPrePrepapre3{},
	&preprepare.Round1PrePrepare{},
	&preprepare.WrongLeaderPrePrepare{},
	&preprepare.FuturePrePrepare{},
	&preprepare.InvalidPrePrepareValue{},

	// prepare
	&prepare.PreparedAtFutureRound{},
	&prepare.PreparedAndDecideAfterChangeRound{},

	// commit
	&commit.DecideDifferentValue{},
	&commit.PrevRoundDecided{},
	&commit.FutureRoundDecided{},

	// change round
	&changeround.ChangeToRound2AndDecide{},
	&changeround.PartialQuorum{},
	&changeround.NotPreparedError{},
	&changeround.PreparedFollowedByPrePrepared{},
	&changeround.FuturePrePrepareAfterChangeRound{},
	&changeround.ChangeRoundThePartialQuorumTheDecide{},

	// common
	&common.DuplicateMessages{},
	&common.InvalidSig{},
	&common.WrongSequenceNumber{},
	&ValidSimpleRun{},
}

func TestAllSpecTests(t *testing.T) {
	require.Len(t, tests, 22)
	for _, test := range tests {
		t.Run(test.Name(), func(tt *testing.T) {
			test.Prepare(tt)
			test.Run(tt)
		})
	}
}
