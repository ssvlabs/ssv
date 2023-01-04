package controller

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v2/qbft"
	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
)

// DefaultInstanceContainerCapacity is the default capacity for InstanceContainer.
const DefaultInstanceContainerCapacity int = 5

type InstanceContainer interface {
	// FindInstance returns the instance with the given height.
	FindInstance(height specqbft.Height) *instance.Instance

	// Instances returns all instances in the container.
	Instances() []*instance.Instance

	// SetInstances sets the instances in the container.
	SetInstances(instances []*instance.Instance)

	// AddNewInstance will add the new instance at index 0, pushing all other stored instanceContainer one index up (ejecting last one if existing)
	AddNewInstance(instance *instance.Instance)
}

type instanceContainer struct {
	instances []*instance.Instance
}

// NewinstanceContainer creates a new instance container and fills it with the given instances.
// capacity represents the upper bound of instanceContainer a processmsg can process messages for as messages are not
// guaranteed to arrive in a timely fashion, we physically limit how far back the processmsg will process messages for.
func NewInstanceContainer(capacity int, instances ...*instance.Instance) InstanceContainer {
	insts := make([]*instance.Instance, capacity)
	copy(insts, instances)

	return &instanceContainer{
		instances: insts,
	}
}

func (c instanceContainer) Instances() []*instance.Instance {
	return c.instances
}

func (c *instanceContainer) SetInstances(instances []*instance.Instance) {
	c.instances = instances
}

func (c instanceContainer) FindInstance(height specqbft.Height) *instance.Instance {
	for _, inst := range c.instances {
		if inst != nil {
			if inst.GetHeight() == height {
				return inst
			}
		}
	}
	return nil
}

func (c *instanceContainer) AddNewInstance(instance *instance.Instance) {
	for idx := DefaultInstanceContainerCapacity - 1; idx > 0; idx-- {
		c.instances[idx] = c.instances[idx-1]
	}
	c.instances[0] = instance
}

type storageInstanceContainer struct {
	InstanceContainer
	logger *zap.Logger

	messageID []byte
	share     *spectypes.Share
	config    qbft.IConfig
}

// NewStorageInstanceContainer returns a hybrid instance container that will first search
// in the given InstanceContainer (which could be in-memory) and then in storage.
func NewStorageInstanceContainer(underlying InstanceContainer, logger *zap.Logger, config qbft.IConfig, share *spectypes.Share, messageID []byte) InstanceContainer {
	return &storageInstanceContainer{
		InstanceContainer: underlying,
		logger:            logger,
		config:            config,
		share:             share,
		messageID:         messageID,
	}
}

func (c storageInstanceContainer) FindInstance(height specqbft.Height) *instance.Instance {
	// Search in underlying.
	if inst := c.InstanceContainer.FindInstance(height); inst != nil {
		return inst
	}

	// Search in storage.
	storedInst, err := c.config.GetStorage().GetInstance(c.messageID, height)
	if err != nil {
		c.logger.Error("could not load instance from storage", zap.Error(err))
		return nil
	}
	if storedInst == nil {
		return nil
	}
	inst := instance.NewInstance(c.config, c.share, c.messageID, storedInst.State.Height)
	inst.State = storedInst.State
	return inst
}
