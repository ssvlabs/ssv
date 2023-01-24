package controller

import (
	"testing"

	"github.com/bloxapp/ssv/protocol/v2/qbft"

	"github.com/stretchr/testify/require"
)

func TestController_Marshaling(t *testing.T) {
	c := qbft.TestingControllerStruct

	byts, err := c.Encode()
	require.NoError(t, err)

	decoded := &Controller{
		// Since StoredInstances is an interface, it wouldn't be decoded properly.
		// Therefore, we set it to NewInstanceContainer which implements json.Unmarshaler
		StoredInstances: make(InstanceContainer, 0, InstanceContainerTestCapacity),
	}
	require.NoError(t, decoded.Decode(byts))

	bytsDecoded, err := decoded.Encode()
	require.NoError(t, err)
	require.EqualValues(t, byts, bytsDecoded)
}
