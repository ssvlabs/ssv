package spectest

import (
	"github.com/bloxapp/ssv/spec/qbft/spectest/tests"
/roundchange"
"testing"
)
type SpecTest interface {
	TestName() string
	Run(t *testing.T)
}

var AllTests = []SpecTest{
	roundchange.HappyFlow(),
}
