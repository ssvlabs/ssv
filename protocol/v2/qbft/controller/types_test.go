package controller

import (
	"math/rand"
	"sort"
	"testing"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
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

func TestInstances_AddNewInstance(t *testing.T) {
	t.Run("add to full", func(t *testing.T) {
		i := append(
			make(InstanceContainer, 0, 5),
			&instance.Instance{State: &specqbft.State{Height: 1}},
			&instance.Instance{State: &specqbft.State{Height: 2}},
			&instance.Instance{State: &specqbft.State{Height: 3}},
			&instance.Instance{State: &specqbft.State{Height: 4}},
			&instance.Instance{State: &specqbft.State{Height: 5}},
		)
		i.addNewInstance(&instance.Instance{State: &specqbft.State{Height: 6}})

		requireHeights(t, i, 6, 1, 2, 3, 4)
	})

	t.Run("add to empty", func(t *testing.T) {
		i := make(InstanceContainer, 0, 5)
		i.addNewInstance(&instance.Instance{State: &specqbft.State{Height: 1}})

		require.EqualValues(t, 1, i[0].State.Height)
		require.Len(t, i, 1)
	})

	t.Run("add to semi full", func(t *testing.T) {
		i := append(
			make(InstanceContainer, 0, 5),
			&instance.Instance{State: &specqbft.State{Height: 1}},
			&instance.Instance{State: &specqbft.State{Height: 2}},
			&instance.Instance{State: &specqbft.State{Height: 3}},
		)
		i.addNewInstance(&instance.Instance{State: &specqbft.State{Height: 4}})

		requireHeights(t, i, 4, 1, 2, 3)
	})

	t.Run("add to full with lower height", func(t *testing.T) {
		i := append(
			make(InstanceContainer, 0, 5),
			&instance.Instance{State: &specqbft.State{Height: 1}},
			&instance.Instance{State: &specqbft.State{Height: 2}},
			&instance.Instance{State: &specqbft.State{Height: 3}},
			&instance.Instance{State: &specqbft.State{Height: 4}},
			&instance.Instance{State: &specqbft.State{Height: 5}},
		)
		i.addNewInstance(&instance.Instance{State: &specqbft.State{Height: 0}})

		requireHeights(t, i, 1, 2, 3, 4, 5)
	})

	t.Run("add to full with higher height", func(t *testing.T) {
		i := append(
			make(InstanceContainer, 0, 5),
			&instance.Instance{State: &specqbft.State{Height: 1}},
			&instance.Instance{State: &specqbft.State{Height: 2}},
			&instance.Instance{State: &specqbft.State{Height: 3}},
			&instance.Instance{State: &specqbft.State{Height: 4}},
			&instance.Instance{State: &specqbft.State{Height: 5}},
		)
		i.addNewInstance(&instance.Instance{State: &specqbft.State{Height: 6}})

		requireHeights(t, i, 6, 1, 2, 3, 4)
	})

	t.Run("add to semi-full with higher height", func(t *testing.T) {
		i := append(
			make(InstanceContainer, 0, 5),
			&instance.Instance{State: &specqbft.State{Height: 9}},
			&instance.Instance{State: &specqbft.State{Height: 7}},
			&instance.Instance{State: &specqbft.State{Height: 5}},
			&instance.Instance{State: &specqbft.State{Height: 1}},
		)
		i.addNewInstance(&instance.Instance{State: &specqbft.State{Height: 4}})
		i.addNewInstance(&instance.Instance{State: &specqbft.State{Height: 3}})
		i.addNewInstance(&instance.Instance{State: &specqbft.State{Height: 6}})
		i.addNewInstance(&instance.Instance{State: &specqbft.State{Height: 8}})
		i.addNewInstance(&instance.Instance{State: &specqbft.State{Height: 8}})

		requireHeights(t, i, 9, 8, 8, 7, 6)
	})

	t.Run("randoms", func(t *testing.T) {
		minCap := InstanceContainerDefaultCapacity/2 + 1
		maxCap := InstanceContainerDefaultCapacity * 2
		for _, capacity := range []int{minCap, InstanceContainerDefaultCapacity, maxCap} {
			// mirror is a slice of heights we've inserted so far.
			// We use it to compare with the InstanceContainer.
			mirror := make([]specqbft.Height, 0, capacity)

			i := make(InstanceContainer, 0, capacity)
			numberOfRandomHeights := 100
			for _, height := range rand.Perm(numberOfRandomHeights) {
				// Add height to InstanceContainer.
				i.addNewInstance(&instance.Instance{State: &specqbft.State{Height: specqbft.Height(height)}})

				// Add height to mirror.
				mirror = append(mirror, specqbft.Height(height))

				// Sort mirror in descending order.
				sort.Slice(mirror, func(i, j int) bool {
					return mirror[i] > mirror[j]
				})

				// Compare with mirror only the first (capacity) elements.
				addedSoFarCap := capacity
				if len(mirror) < capacity {
					addedSoFarCap = len(mirror)
				}
				requireHeights(t, i, mirror[:addedSoFarCap]...)
			}

			// Finally, a sanity check. We expect the InstanceContainer to contain exactly
			// the numbers from numberOfRandomHeights-1 to numberOfRandomHeights-capacity.
			expectedHeights := make([]specqbft.Height, 0, capacity)
			minHeight := 0
			if capacity < numberOfRandomHeights {
				minHeight = numberOfRandomHeights - capacity
			}
			for height := numberOfRandomHeights - 1; height >= minHeight; height-- {
				expectedHeights = append(expectedHeights, specqbft.Height(height))
			}
			requireHeights(t, i, expectedHeights...)
		}
	})
}

func requireHeights(t *testing.T, i []*instance.Instance, heights ...specqbft.Height) {
	actualHeights := make([]specqbft.Height, 0, len(heights))
	for _, v := range i {
		actualHeights = append(actualHeights, v.State.Height)
	}
	require.EqualValues(t, heights, actualHeights, "expected heights to match")
}
