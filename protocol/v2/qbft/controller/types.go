package controller

import (
	"encoding/json"
	"fmt"
	"strings"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"

	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
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

// String returns a human-readable representation of the instances. Useful for debugging.
func (i *InstanceContainer) String() string {
	heights := make([]string, len(*i))
	for index, inst := range *i {
		heights[index] = fmt.Sprint(inst.GetHeight())
	}
	return fmt.Sprintf("Instances(len=%d, cap=%d, heights=(%s))", len(*i), cap(*i), strings.Join(heights, ", "))
}

// UnmarshalJSON implements the json.Unmarshaler interface for InstanceContainer
func (c *InstanceContainer) UnmarshalJSON(data []byte) error {
	// InstanceContainer must always have correct capacity on initialization
	// because addition to instance container doesn't grow beyond cap removing values that don't fit.
	// Therefore, we need to initialize it properly on unmarshalling
	// to allow spec tests grow StoredInstances as much as they need to.
	instances := make([]*instance.Instance, 0, InstanceContainerTestCapacity)
	if cap(*c) != 0 {
		instances = *c
	}

	if err := json.Unmarshal(data, &instances); err != nil {
		return err
	}

	*c = instances

	return nil
}
