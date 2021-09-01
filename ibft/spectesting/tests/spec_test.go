package tests

import (
	"testing"
)

type SpecTest interface {
	Name() string
	// Prepare sets all testing fixtures and params before running the test
	Prepare(t *testing.T)
	// Run will execute the test.
	Run(t *testing.T)
}

var tests = []SpecTest{
	&PrepareAtFutureRound{},
	&ChangeRoundAndDecide{},
	&PrepareChangeRoundAndDecide{},
	&DecideDifferentValue{},
	&PrepareAtFutureRound{},
	&NonJustifiedPrePrepapre{},
	&DuplicateMessages{},
	&ValidSimpleRun{},
	&ChangeRoundPartialQuorum{},
	&PrevRoundDecided{},
}

func TestAllSpecTests(t *testing.T) {
	for _, test := range tests {
		t.Run(test.Name(), func(tt *testing.T) {
			test.Prepare(tt)
			test.Run(tt)
		})
	}
}
