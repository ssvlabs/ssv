package tests

import (
	"github.com/bloxapp/ssv/spec/dkg"
	"testing"
)

type MsgProcessingSpecTest struct {
	Name     string
	Messages []*dkg.SignedMessage
}

func (test *MsgProcessingSpecTest) TestName() string {
	return test.Name
}

func (test *MsgProcessingSpecTest) Run(t *testing.T) {

}
