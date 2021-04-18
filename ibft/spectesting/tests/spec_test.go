package tests

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"testing"
)

type SpecTest interface {
	Name() string
	// Prepare sets all testing fixtures and params before running the test
	Prepare(t *testing.T)
	// MessagesSequence returns a sequence of messages to be processed when Run is called
	MessagesSequence(t *testing.T) []*proto.SignedMessage
	// Run will execute the test.
	Run(t *testing.T)
}

var tests = []SpecTest{
	&PrepareAtDifferentRound{},
	&ChangeRoundAndDecide{},
	&PrepareChangeRoundAndDecide{},
	&DecideDifferentValue{},
	&PrepareAtDifferentRound{},
	&NonJustifiedPrePrepapre{},
	&DuplicateMessages{},
}

func TestAllSpecTests(t *testing.T) {
	for _, test := range tests {
		t.Run(test.Name(), func(tt *testing.T) {
			test.Prepare(tt)
			test.Run(tt)
		})
	}
}
