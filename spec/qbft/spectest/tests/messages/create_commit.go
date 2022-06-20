package messages

import "github.com/bloxapp/ssv/spec/qbft/spectest/tests"
"

// CreateCommit tests creating a commit msg
func CreateCommit() *tests.CreateMsgSpecTest {
	return &tests.CreateMsgSpecTest{
		CreateType:   tests.CreateCommit,
		Name:         "create commit",
		Value:        []byte{1, 2, 3, 4},
		Round:        10,
		ExpectedRoot: "06c09ba2774b7581bad2774a3f57c2002954826b0133cf1e9b6e774a85bcc7cd",
	}
}
