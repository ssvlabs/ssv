package spectest

import "testing"

type MultiMsgProcessingSpecTest struct {
	Name  string
	Tests []*MsgProcessingSpecTest
}

func (tests *MultiMsgProcessingSpecTest) TestName() string {
	return tests.Name
}

func (tests *MultiMsgProcessingSpecTest) Run(t *testing.T) {
	for _, test := range tests.Tests {
		test := test
		t.Run(test.TestName(), func(t *testing.T) {
			RunMsgProcessing(t, test)
		})
	}
}
