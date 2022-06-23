package messages

import "github.com/bloxapp/ssv/spec/qbft/spectest/tests"

// CreatePrepare tests creating a prepare msg
func CreatePrepare() *tests.CreateMsgSpecTest {
	return &tests.CreateMsgSpecTest{
		CreateType:   tests.CreatePrepare,
		Name:         "create prepare",
		Value:        []byte{1, 2, 3, 4},
		Round:        10,
		ExpectedRoot: "e0449c0106826cbea29cbd2e8269782fc362146d6386790d44dc155edde63301",
	}
}
