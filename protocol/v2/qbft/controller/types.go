package controller

import (
	"fmt"
	"log"
	"strings"

	specqbft "github.com/bloxapp/ssv-spec/qbft"

	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
)

const (
	// DefaultInstanceContainerCapacity is the default capacity for InstanceContainer.
	// This is used when unmarshaling or making an InstanceContainer slice without capacaity.
	// Keep this value fit for spec tests to pass.
	DefaultInstanceContainerCapacity int = 1024

	// RuntimeInstanceContainerCapacity is the capacity for InstanceContainer when running a node
	// with storage.
	RuntimeInstanceContainerCapacity int = 5
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

// addNewInstance will add the new instance at index 0, pushing all other stored InstanceContainer one index up (ejecting last one if existing)
func (i *InstanceContainer) addNewInstance(instance *instance.Instance) {
	if cap(*i) == 0 {
		*i = make(InstanceContainer, 0, DefaultInstanceContainerCapacity)
	}

	log.Printf("adding new instance %d, snapshot before: %s", instance.GetHeight(), i)

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

	log.Printf("added instance %d, snapshot after: %s", instance.GetHeight(), i)
}

// reset will remove all instances from the container, perserving the underlying slice's capacity.
func (i *InstanceContainer) reset() {
	*i = (*i)[:0]
}

func (i *InstanceContainer) String() string {
	heights := make([]string, len(*i))
	for index, inst := range *i {
		heights[index] = fmt.Sprint(inst.GetHeight())
	}
	return fmt.Sprintf("Instances(len=%d, cap=%d, heights=(%s))", len(*i), cap(*i), strings.Join(heights, ", "))
}
