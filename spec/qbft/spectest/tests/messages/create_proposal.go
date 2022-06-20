package messages

import (
	"github.com/bloxapp/ssv/spec/qbft/spectest/tests"
"
)

// CreateProposal tests creating a proposal msg, not previously prepared
func CreateProposal() *tests.CreateMsgSpecTest {
	return &tests.CreateMsgSpecTest{
		CreateType:   tests.CreateProposal,
		Name:         "create proposal",
		Value:        []byte{1, 2, 3, 4},
		ExpectedRoot: "7aff149aa49b4dd364e108a0abbf78eae78772eff2ddaed2f2a61bca771077fd",
	}
}
