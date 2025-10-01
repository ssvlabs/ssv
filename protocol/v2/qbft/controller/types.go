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

func (ic *InstanceContainer) FindInstance(height specqbft.Height) *instance.Instance {
	for _, inst := range *ic {
		if inst != nil {
			if inst.GetHeight() == height {
				return inst
			}
		}
	}
	return nil
}

// addNewInstance will add the new instance at index 0, pushing all other stored InstanceContainer one index up
// (ejecting the last one if necessary)
func (ic *InstanceContainer) addNewInstance(instance *instance.Instance) {
	if cap(*ic) == 0 {
		*ic = make(InstanceContainer, 0, InstanceContainerDefaultCapacity)
	}

	indexToInsert := len(*ic)
	for index, existingInstance := range *ic {
		if existingInstance.GetHeight() < instance.GetHeight() {
			indexToInsert = index
			break
		}
	}

	if indexToInsert == len(*ic) {
		// Append the new instance, only if there's free space for it.
		if len(*ic) < cap(*ic) {
			*ic = append(*ic, instance)
		}
	} else {
		if len(*ic) == cap(*ic) {
			// Shift the instances to the right and insert the new instance.
			copy((*ic)[indexToInsert+1:], (*ic)[indexToInsert:])
			(*ic)[indexToInsert] = instance
		} else {
			// Shift the instances to the right and append the new instance.
			*ic = append(*ic, nil)
			copy((*ic)[indexToInsert+1:], (*ic)[indexToInsert:])
			(*ic)[indexToInsert] = instance
		}
	}
}

// String returns a human-readable representation of the instances. Useful for debugging.
func (ic *InstanceContainer) String() string {
	heights := make([]string, len(*ic))
	for index, inst := range *ic {
		heights[index] = fmt.Sprint(inst.GetHeight())
	}
	return fmt.Sprintf("Instances(len=%d, cap=%d, heights=(%s))", len(*ic), cap(*ic), strings.Join(heights, ", "))
}

// UnmarshalJSON implements the json.Unmarshaler interface for InstanceContainer
func (ic *InstanceContainer) UnmarshalJSON(data []byte) error {
	// InstanceContainer must always have correct capacity on initialization
	// because addition to instance container doesn't grow beyond cap removing values that don't fit.
	// Therefore, we need to initialize it properly on unmarshaling
	// to allow spec tests grow StoredInstances as much as they need to.
	instances := make([]*instance.Instance, 0, InstanceContainerTestCapacity)
	if cap(*ic) != 0 {
		instances = *ic
	}

	if err := json.Unmarshal(data, &instances); err != nil {
		return err
	}

	*ic = instances

	return nil
}
