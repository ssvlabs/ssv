package instance

import (
	"github.com/bloxapp/ssv/protocol/v2/qbft"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestInstance_Marshaling(t *testing.T) {
	i := qbft.TestingInstanceStruct

	byts, err := i.Encode()
	require.NoError(t, err)

	decoded := &Instance{}
	require.NoError(t, decoded.Decode(byts))

	bytsDecoded, err := decoded.Encode()
	require.NoError(t, err)
	require.EqualValues(t, byts, bytsDecoded)
}
