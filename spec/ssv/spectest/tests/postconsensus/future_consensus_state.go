package postconsensus

import (
	"github.com/bloxapp/ssv/spec/ssv/spectest/tests"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/bloxapp/ssv/spec/types/testingutils"
)

// FutureConsensusState tests msg for a future valcheck state
func FutureConsensusState() *tests.SpecTest {
	ks := testingutils.Testing4SharesSet()
	dr := testingutils.DecidedRunner(ks)

	msgs := []*types.SSVMessage{
		testingutils.SSVMsgAttester(nil, testingutils.PostConsensusAttestationMsg(ks.Shares[1], 1, 10)),
	}

	return &tests.SpecTest{
		Name:                    "future SignedPostConsensusMessage",
		Runner:                  dr,
		Messages:                msgs,
		PostDutyRunnerStateRoot: "cbcefe579470d914c3c230bd45cee06e9c5723460044b278a0c629a742551b02",
		ExpectedError:           "PartialSignatureMessage Height doesn't match duty runner's Height",
	}
}
