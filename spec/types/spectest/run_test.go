package spectest

import (
	"github.com/bloxapp/ssv/spec/types"
	"github.com/bloxapp/ssv/spec/types/spectest/tests"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAll(t *testing.T) {
	for _, test := range AllTests {
		t.Run(test.Name, func(t *testing.T) {
			runTest(t, test)
		})
	}
}

func runTest(t *testing.T, test *tests.EncodingSpecTest) {
	switch test.DataType {
	case tests.ConsensusDataType:
		a := types.ConsensusData{}
		require.NoError(t, a.Decode(test.Data))

		byts, err := a.Encode()
		require.NoError(t, err)
		require.EqualValues(t, test.Data, byts)
	default:
		t.Fail()
	}
}
