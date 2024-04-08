package controller

import (
	"fmt"
	"strings"

	specqbft "github.com/bloxapp/ssv-spec/qbft"

	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
)

const (
	// InstanceContainerDefaultCapacity is the default capacity for InstanceContainer.
	InstanceContainerDefaultCapacity int = 2

	// InstanceContainerTestCapacity is the capacity for InstanceContainer used in tests.
	InstanceContainerTestCapacity int = 1024
)

// InstanceContainer is a fixed-capacity container for instances.
type InstanceContainer []*instance.Instance

func (i InstanceContainer) FindInstance(height specqbft.Height) *instance.Instance {
	for _, inst := range i {
		if inst != nil {
			if inst.GetHeight() == height {
				return inst
			}
		}
	}
	return nil
}

// AddNewInstance will add the new instance at index 0, pushing all other stored InstanceContainer one index up (ejecting last one if existing)
func (i *InstanceContainer) AddNewInstance(instance *instance.Instance) {
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

	if indexToInsert == len(*i) {
		// Append the new instance, only if there's free space for it.
		if len(*i) < cap(*i) {
			*i = append(*i, instance)
		}
	} else {
		if len(*i) == cap(*i) {
			// Shift the instances to the right and insert the new instance.
			copy((*i)[indexToInsert+1:], (*i)[indexToInsert:])
			(*i)[indexToInsert] = instance
		} else {
			// Shift the instances to the right and append the new instance.
			*i = append(*i, nil)
			copy((*i)[indexToInsert+1:], (*i)[indexToInsert:])
			(*i)[indexToInsert] = instance
		}
	}
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
