package qbft

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestInstances_FindInstance(t *testing.T) {
	i := InstanceContainer{
		&Instance{State: &State{Height: 1}},
		&Instance{State: &State{Height: 2}},
		&Instance{State: &State{Height: 3}},
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
			&Instance{State: &State{Height: 1}},
			&Instance{State: &State{Height: 2}},
			&Instance{State: &State{Height: 3}},
			&Instance{State: &State{Height: 4}},
			&Instance{State: &State{Height: 5}},
		}
		i.addNewInstance(&Instance{State: &State{Height: 6}})

		require.EqualValues(t, 6, i[0].State.Height)
		require.EqualValues(t, 1, i[1].State.Height)
		require.EqualValues(t, 2, i[2].State.Height)
		require.EqualValues(t, 3, i[3].State.Height)
		require.EqualValues(t, 4, i[4].State.Height)
	})

	t.Run("add to empty", func(t *testing.T) {
		i := InstanceContainer{}
		i.addNewInstance(&Instance{State: &State{Height: 1}})

		require.EqualValues(t, 1, i[0].State.Height)
		require.Nil(t, i[1])
		require.Nil(t, i[2])
		require.Nil(t, i[3])
		require.Nil(t, i[4])
	})

	t.Run("add to semi full", func(t *testing.T) {
		i := InstanceContainer{
			&Instance{State: &State{Height: 1}},
			&Instance{State: &State{Height: 2}},
			&Instance{State: &State{Height: 3}},
		}
		i.addNewInstance(&Instance{State: &State{Height: 4}})

		require.EqualValues(t, 4, i[0].State.Height)
		require.EqualValues(t, 1, i[1].State.Height)
		require.EqualValues(t, 2, i[2].State.Height)
		require.EqualValues(t, 3, i[3].State.Height)
		require.Nil(t, i[4])
	})
}

func TestController_Marshaling(t *testing.T) {
	c := testingControllerStruct

	byts, err := c.Encode()
	require.NoError(t, err)

	decoded := &Controller{}
	require.NoError(t, decoded.Decode(byts))

	bytsDecoded, err := decoded.Encode()
	require.NoError(t, err)
	require.EqualValues(t, byts, bytsDecoded)
}
