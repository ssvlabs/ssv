package controller

import (
	"encoding/hex"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"

	"github.com/DmitriyVTitov/size"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
	qbftstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	"github.com/cornelk/hashmap"
	"go.uber.org/zap"

	"github.com/pkg/errors"
)

var (
	alreadyLoaded = hashmap.New[string, bool]()
	sizeLock      sync.Mutex
	total         atomic.Int64
	// runID is a random string
	runID = fmt.Sprintf("%#x", rand.Int63())
)

func (c *Controller) LoadHighestInstance(identifier []byte) error {
	highestInstance, err := c.getHighestInstance(identifier[:])
	if err != nil {
		return err
	}
	if highestInstance == nil {
		return nil
	}
	c.Height = highestInstance.GetHeight()
	c.StoredInstances.reset()
	c.StoredInstances.addNewInstance(highestInstance)
	return nil
}

func (c *Controller) getHighestInstance(identifier []byte) (*instance.Instance, error) {
	highestInstance, err := c.config.GetStorage().GetHighestInstance(identifier)
	if err != nil {
		return nil, errors.Wrap(err, "could not fetch highest instance")
	}
	if highestInstance == nil {
		return nil, nil
	}

	strIdentifier := string(identifier)
	sizeLock.Lock()
	sizeOfInstance := size.Of(highestInstance)
	sizeLock.Unlock()
	if _, ok := alreadyLoaded.Get(strIdentifier); !ok {
		alreadyLoaded.Set(strIdentifier, true)
		total.Add(int64(sizeOfInstance))
	}

	c.logger.Debug("loadedhighestinstance",
		zap.String("identifier", hex.EncodeToString(identifier)),
		zap.Int64("total", total.Load()),
		zap.Int("size", sizeOfInstance),
		zap.String("runID", runID),
	)

	// ii := deepcopy.Copy(highestInstance)
	// highestInstance = ii.(*qbftstorage.StoredInstance)

	i := instance.NewInstance(
		c.config,
		highestInstance.State.Share,
		identifier,
		highestInstance.State.Height,
	)
	i.State = highestInstance.State

	return i, nil
}

// SaveInstance saves the given instance to the storage.
func (c *Controller) SaveInstance(i *instance.Instance, msg *specqbft.SignedMessage) error {
	storedInstance := &qbftstorage.StoredInstance{
		State:          i.State,
		DecidedMessage: msg,
	}
	isHighest := msg.Message.Height >= c.Height

	// Full nodes save both highest and historical instances.
	if c.fullNode {
		if isHighest {
			return c.config.GetStorage().SaveHighestAndHistoricalInstance(storedInstance)
		}
		return c.config.GetStorage().SaveInstance(storedInstance)
	}

	// Light nodes only save highest instances.
	if isHighest {
		return c.config.GetStorage().SaveHighestInstance(storedInstance)
	}

	return nil
}
