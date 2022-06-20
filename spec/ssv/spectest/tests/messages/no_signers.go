package messages

import (
	"github.com/bloxapp/ssv-spec/ssv/spectest/tests"
	"github.com/bloxapp/ssv/spec/qbft"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/bloxapp/ssv/spec/types/testingutils"
)

// NoSigners tests an empty SignedPostConsensusMessage Signers
func NoSigners() *tests.SpecTest {
	ks := testingutils.Testing4SharesSet()
	dr := testingutils.DecidedRunner(ks)

	noSignerMsg := testingutils.PostConsensusAttestationMsg(ks.Shares[1], 1, qbft.FirstHeight)
	noSignerMsg.Signers = []types.OperatorID{}
	msgs := []*types.SSVMessage{
		testingutils.SSVMsgAttester(nil, noSignerMsg),
	}

	return &tests.SpecTest{
		Name:                    "NoSigners SignedPostConsensusMessage",
		Runner:                  dr,
		Messages:                msgs,
		PostDutyRunnerStateRoot: "cbcefe579470d914c3c230bd45cee06e9c5723460044b278a0c629a742551b02",
		ExpectedError:           "partial post valcheck sig invalid: SignedPartialSignatureMessage invalid: no SignedPartialSignatureMessage signers",
	}
}
