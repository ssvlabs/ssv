package messages

import "github.com/bloxapp/ssv/spec/qbft/spectest/tests"

// CreateRoundChange tests creating a round change msg, not previously prepared
func CreateRoundChange() *tests.CreateMsgSpecTest {
	return &tests.CreateMsgSpecTest{
		CreateType:   tests.CreateRoundChange,
		Name:         "create round change",
		Value:        []byte{1, 2, 3, 4},
		ExpectedRoot: "568186a0219572528365cf31effc4cdc86cbaf2e88f63316054cf0e0a3d92ba6",
	}
}
