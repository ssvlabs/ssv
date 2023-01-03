package controller

import (
	"github.com/bloxapp/ssv/protocol/v2/qbft"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestController_Marshaling(t *testing.T) {
	c := qbft.TestingControllerStruct

	byts, err := c.Encode()
	require.NoError(t, err)

	decoded := &Controller{}
	require.NoError(t, decoded.Decode(byts))

	bytsDecoded, err := decoded.Encode()
	require.NoError(t, err)
	require.EqualValues(t, byts, bytsDecoded)
}
