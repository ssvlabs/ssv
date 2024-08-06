package controller

import (
	"fmt"
	"strings"

	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"

	"github.com/ssvlabs/ssv/protocol/genesis/qbft/instance"
)

const (
	// InstanceContainerDefaultCapacity is the default capacity for InstanceContainer.
	InstanceContainerDefaultCapacity int = 2

	// InstanceContainerTestCapacity is the capacity for InstanceContainer used in tests.
	InstanceContainerTestCapacity int = 1024
)

// InstanceContainer is a fixed-capacity container for instances.
type InstanceContainer []*instance.Instance

func (i InstanceContainer) FindInstance(height genesisspecqbft.Height) *instance.Instance {
	for _, inst := range i {
		if inst != nil {
			if inst.GetHeight() == height {
				return inst
			}
		}
	}
	return nil
}

// addNewInstance will add the new instance at index 0, pushing all other stored InstanceContainer one index up (ejecting last one if existing)
func (i *InstanceContainer) addNewInstance(instance *instance.Instance) {
	if cap(*i) == 0 {
		*i = make(InstanceContainer, 0, InstanceContainerDefaultCapacity)
	}

	indexToInsert := len(*i)
	for index, existingInstance := range *i {
		if existingInstance.GetHeight() < instance.GetHeight() {
			indexToInsert = index
			break
		}
	}
	*i = insertAtIndex(*i, indexToInsert, instance)
}

func insertAtIndex(arr []*instance.Instance, index int, value *instance.Instance) InstanceContainer {
	if len(arr) == index { // nil or empty slice or after last element
		return append(arr, value)
	}
	arr = append(arr[:index+1], arr[index:]...) // index < len(a)
	arr[index] = value
	return arr
}

// reset will remove all instances from the container, perserving the underlying slice's capacity.
func (i *InstanceContainer) reset() {
	*i = (*i)[:0]
}

// String returns a human-readable representation of the instances. Useful for debugging.
func (i *InstanceContainer) String() string {
	heights := make([]string, len(*i))
	for index, inst := range *i {
		heights[index] = fmt.Sprint(inst.GetHeight())
	}
	return fmt.Sprintf("Instances(len=%d, cap=%d, heights=(%s))", len(*i), cap(*i), strings.Join(heights, ", "))
}
