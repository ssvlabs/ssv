package spectest

import (
	"github.com/bloxapp/ssv/spec/types/spectest/tests"
	"github.com/bloxapp/ssv/spec/types/spectest/tests/consensusdata"
)

var AllTests = []*tests.EncodingSpecTest{
	consensusdata.Encoding(),
}
