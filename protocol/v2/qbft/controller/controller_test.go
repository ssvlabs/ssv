package controller

import (
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/protocol/v2/qbft"
	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
)

func TestInstances_FindInstance(t *testing.T) {
	i := InstanceContainer{
		&instance.Instance{State: &specqbft.State{Height: 1}},
		&instance.Instance{State: &specqbft.State{Height: 2}},
		&instance.Instance{State: &specqbft.State{Height: 3}},
	}

	t.Run("find 1", func(t *testing.T) {
		require.NotNil(t, i.FindInstance(1))
	})
	t.Run("find 2", func(t *testing.T) {
		require.NotNil(t, i.FindInstance(2))
	})
	t.Run("find 5", func(t *testing.T) {
		require.Nil(t, i.FindInstance(5))
	})
}

func TestInstances_addNewInstance(t *testing.T) {
	t.Run("add to full", func(t *testing.T) {
		i := InstanceContainer{
			&instance.Instance{State: &specqbft.State{Height: 1}},
			&instance.Instance{State: &specqbft.State{Height: 2}},
			&instance.Instance{State: &specqbft.State{Height: 3}},
			&instance.Instance{State: &specqbft.State{Height: 4}},
			&instance.Instance{State: &specqbft.State{Height: 5}},
		}
		i.AddNewInstance(&instance.Instance{State: &specqbft.State{Height: 6}})

		require.EqualValues(t, 6, i[0].State.Height)
		require.EqualValues(t, 1, i[1].State.Height)
		require.EqualValues(t, 2, i[2].State.Height)
		require.EqualValues(t, 3, i[3].State.Height)
		require.EqualValues(t, 4, i[4].State.Height)
	})

	t.Run("add to empty", func(t *testing.T) {
		i := InstanceContainer{}
		i.AddNewInstance(&instance.Instance{State: &specqbft.State{Height: 1}})

		require.EqualValues(t, 1, i[0].State.Height)
		require.Nil(t, i[1])
		require.Nil(t, i[2])
		require.Nil(t, i[3])
		require.Nil(t, i[4])
	})

	t.Run("add to semi full", func(t *testing.T) {
		i := InstanceContainer{
			&instance.Instance{State: &specqbft.State{Height: 1}},
			&instance.Instance{State: &specqbft.State{Height: 2}},
			&instance.Instance{State: &specqbft.State{Height: 3}},
		}
		i.AddNewInstance(&instance.Instance{State: &specqbft.State{Height: 4}})

		require.EqualValues(t, 4, i[0].State.Height)
		require.EqualValues(t, 1, i[1].State.Height)
		require.EqualValues(t, 2, i[2].State.Height)
		require.EqualValues(t, 3, i[3].State.Height)
		require.Nil(t, i[4])
	})
}

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
