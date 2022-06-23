package postconsensus

import (
	"github.com/bloxapp/ssv/spec/qbft"
	"github.com/bloxapp/ssv/spec/ssv/spectest/tests"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/bloxapp/ssv/spec/types/testingutils"
)

// InvaliSignature tests an invalid SignedPostConsensusMessage sig
func InvaliSignature() *tests.SpecTest {
	ks := testingutils.Testing4SharesSet()
	dr := testingutils.DecidedRunner(ks)

	msgs := []*types.SSVMessage{
		testingutils.SSVMsgAttester(nil, testingutils.PostConsensusAttestationMsg(ks.Shares[1], 2, qbft.FirstHeight)),
	}

	return &tests.SpecTest{
		Name:                    "Invalid SignedPostConsensusMessage signature",
		Runner:                  dr,
		Messages:                msgs,
		PostDutyRunnerStateRoot: "cbcefe579470d914c3c230bd45cee06e9c5723460044b278a0c629a742551b02",
		ExpectedError:           "partial post valcheck sig invalid: failed to verify PartialSignature: failed to verify signature",
	}
}
