package qbft

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/bloxapp/ssv/spec/types"
	"github.com/pkg/errors"
)

// HistoricalInstanceCapacity represents the upper bound of InstanceContainer a controller can process messages for as messages are not
// guaranteed to arrive in a timely fashion, we physically limit how far back the controller will process messages for
const HistoricalInstanceCapacity int = 5

type InstanceContainer [HistoricalInstanceCapacity]*Instance

func (i InstanceContainer) FindInstance(height Height) *Instance {
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
func (i *InstanceContainer) addNewInstance(instance *Instance) {
	for idx := HistoricalInstanceCapacity - 1; idx > 0; idx-- {
		i[idx] = i[idx-1]
	}
	i[0] = instance
}

// Controller is a QBFT coordinator responsible for starting and following the entire life cycle of multiple QBFT InstanceContainer
type Controller struct {
	Identifier []byte
	Height     Height // incremental Height for InstanceContainer
	// StoredInstances stores the last HistoricalInstanceCapacity in an array for message processing purposes.
	StoredInstances InstanceContainer
	Domain          types.DomainType
	Share           *types.Share
	signer          types.SSVSigner
	valueCheck      ProposedValueCheck
	storage         Storage
	network         Network
}

func NewController(
	identifier []byte,
	share *types.Share,
	domain types.DomainType,
	signer types.SSVSigner,
	valueCheck ProposedValueCheck,
	storage Storage,
	network Network,
) *Controller {
	return &Controller{
		Identifier:      identifier,
		Height:          -1, // as we bump the height when starting the first instance
		Domain:          domain,
		Share:           share,
		StoredInstances: InstanceContainer{},
		signer:          signer,
		valueCheck:      valueCheck,
		storage:         storage,
		network:         network,
	}
}

// StartNewInstance will start a new QBFT instance, if can't will return error
func (c *Controller) StartNewInstance(value []byte) error {
	if err := c.canStartInstance(value); err != nil {
		return errors.Wrap(err, "can't start new QBFT instance")
	}

	c.bumpHeight()
	newInstance := c.addAndStoreNewInstance()
	newInstance.Start(value, c.Height)

	return nil
}

// ProcessMsg processes a new msg, returns true if Decided, non nil byte slice if Decided (Decided value) and error
// Decided returns just once per instance as true, following messages (for example additional commit msgs) will not return Decided true
func (c *Controller) ProcessMsg(msg *SignedMessage) (bool, []byte, error) {
	if !bytes.Equal(c.Identifier, msg.Message.Identifier) {
		return false, nil, errors.New(fmt.Sprintf("message doesn't belong to Identifier %x", c.Identifier))
	}

	inst := c.InstanceForHeight(msg.Message.Height)
	if inst == nil {
		return false, nil, errors.New(fmt.Sprintf("instance for Height %d,  Identifier %x not found", msg.Message.Height, c.Identifier))
	}

	prevDecided, _ := inst.IsDecided()
	decided, decidedValue, aggregatedCommit, err := inst.ProcessMsg(msg)
	if err != nil {
		return false, nil, errors.Wrap(err, "could not process msg")
	}

	// if previously Decided we do not return Decided true again
	if prevDecided {
		return false, nil, err
	}

	// save the highest Decided
	if !decided {
		return false, nil, nil
	}

	if err := c.storage.SaveHighestDecided(aggregatedCommit); err != nil {
		// TODO - LOG
	}

	// Broadcast Decided msg
	decidedMsg := &DecidedMessage{
		SignedMessage: aggregatedCommit,
	}
	if err := c.network.BroadcastDecided(decidedMsg); err != nil {
		// We do not return error here, just Log broadcasting error.
		return decided, decidedValue, nil
	}

	return decided, decidedValue, nil
}

func (c *Controller) InstanceForHeight(height Height) *Instance {
	return c.StoredInstances.FindInstance(height)
}

func (c *Controller) bumpHeight() {
	c.Height++
}

// GetIdentifier returns QBFT Identifier, used to identify messages
func (c *Controller) GetIdentifier() []byte {
	return c.Identifier
}

// addAndStoreNewInstance returns creates a new QBFT instance, stores it in an array and returns it
func (c *Controller) addAndStoreNewInstance() *Instance {
	i := NewInstance(c.generateConfig(), c.Share, c.Identifier, c.Height)
	c.StoredInstances.addNewInstance(i)
	return i
}

func (c *Controller) canStartInstance(value []byte) error {
	if c.Height > FirstHeight {
		// check prev instance if prev instance is not the first instance
		inst := c.StoredInstances.FindInstance(c.Height)
		if inst == nil {
			return errors.New("could not find previous instance")
		}
		if decided, _ := inst.IsDecided(); !decided {
			return errors.New("previous instance hasn't Decided")
		}
	}

	// check value
	if err := c.valueCheck(value); err != nil {
		return errors.Wrap(err, "value invalid")
	}

	return nil
}

// Encode implementation
func (c *Controller) Encode() ([]byte, error) {
	return json.Marshal(c)
}

// Decode implementation
func (c *Controller) Decode(data []byte) error {
	err := json.Unmarshal(data, &c)
	if err != nil {
		return errors.Wrap(err, "could not decode controller")
	}

	config := c.generateConfig()
	for _, i := range c.StoredInstances {
		if i != nil {
			i.config = config
		}
	}
	return nil
}

func (c *Controller) generateConfig() IConfig {
	return &Config{
		Signer:     c.signer,
		SigningPK:  c.Share.ValidatorPubKey,
		Domain:     c.Domain,
		ValueCheck: c.valueCheck,
		Storage:    c.storage,
		Network:    c.network,
	}
}
