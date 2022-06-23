package spectest

import (
	"testing"

	"github.com/bloxapp/ssv/spec/qbft/spectest/tests/roundchange"
)

type SpecTest interface {
	TestName() string
	Run(t *testing.T)
}

var AllTests = []SpecTest{
	roundchange.HappyFlow(),
}
