package controller

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	qbftspec "github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
	types2 "github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/pkg/errors"
)

// HistoricalInstanceCapacity represents the upper bound of InstanceContainer a processmsg can process messages for as messages are not
// guaranteed to arrive in a timely fashion, we physically limit how far back the processmsg will process messages for
const HistoricalInstanceCapacity int = 5

type InstanceContainer [HistoricalInstanceCapacity]*instance.Instance

func (i InstanceContainer) FindInstance(height qbftspec.Height) *instance.Instance {
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
	for idx := HistoricalInstanceCapacity - 1; idx > 0; idx-- {
		i[idx] = i[idx-1]
	}
	i[0] = instance
}

// Controller is a QBFT coordinator responsible for starting and following the entire life cycle of multiple QBFT InstanceContainer
type Controller struct {
	Identifier []byte
	Height     qbftspec.Height // incremental Height for InstanceContainer
	// StoredInstances stores the last HistoricalInstanceCapacity in an array for message processing purposes.
	StoredInstances InstanceContainer
	// FutureMsgsContainer holds all msgs from a higher height
	FutureMsgsContainer map[types.OperatorID]qbftspec.Height // maps msg signer to height of higher height received msgs
	Domain              types.DomainType
	Share               *types.Share
	config              types2.IConfig

	// TODO: state
	// TODO: validation pipeline
}

func NewController(
	identifier []byte,
	share *types.Share,
	domain types.DomainType,
	config types2.IConfig,
) *Controller {
	return &Controller{
		Identifier:          identifier,
		Height:              -1, // as we bump the height when starting the first instance
		Domain:              domain,
		Share:               share,
		StoredInstances:     InstanceContainer{},
		FutureMsgsContainer: make(map[types.OperatorID]qbftspec.Height),
		config:              config,
	}
}

func (c *Controller) IsReady() bool {
	// TODO: check state
	return true
}

func (c *Controller) Sync() bool {
	// TODO: check state
	return true
}

// StartNewInstance will start a new QBFT instance, if can't will return error
func (c *Controller) StartNewInstance(value []byte) error {
	if err := c.canStartInstance(c.Height+1, value); err != nil {
		return errors.Wrap(err, "can't start new QBFT instance")
	}

	c.bumpHeight()
	newInstance := c.addAndStoreNewInstance()
	newInstance.Start(value, c.Height)

	return nil
}

// ProcessMsg processes a new msg, returns decided message or error
func (c *Controller) ProcessMsg(msg *qbftspec.SignedMessage) (*qbftspec.SignedMessage, error) {
	if err := c.baseMsgValidation(msg); err != nil {
		return nil, errors.Wrap(err, "invalid msg")
	}

	/**
	Main controller processing flow
	_______________________________
	All decided msgs are processed the same, out of instance
	All valid future msgs are saved in a container and can trigger highest decided futuremsg
	All other msgs (not future or decided) are processed normally by an existing instance (if found)
	*/
	if isDecidedMsg(c.Share, msg) {
		return c.UponDecided(msg)
	} else if msg.Message.Height > c.Height {
		return c.UponFutureMsg(msg)
	} else {
		return c.UponExistingInstanceMsg(msg)
	}
}

func (c *Controller) UponExistingInstanceMsg(msg *qbftspec.SignedMessage) (*qbftspec.SignedMessage, error) {
	inst := c.InstanceForHeight(msg.Message.Height)
	if inst == nil {
		return nil, errors.New("instance not found")
	}

	prevDecided, _ := inst.IsDecided()

	decided, _, decidedMsg, err := inst.ProcessMsg(msg)
	if err != nil {
		return nil, errors.Wrap(err, "could not process msg")
	}

	// if previously Decided we do not return Decided true again
	if prevDecided {
		return nil, err
	}

	// save the highest Decided
	if !decided {
		return nil, nil
	}

	if err := c.saveAndBroadcastDecided(decidedMsg); err != nil {
		// no need to fail processing instance deciding if failed to save/ broadcast
		fmt.Printf("%s\n", err.Error())
	}
	return msg, nil
}

func (c *Controller) baseMsgValidation(msg *qbftspec.SignedMessage) error {
	// verify msg belongs to controller
	if !bytes.Equal(c.Identifier, msg.Message.Identifier) {
		return errors.New("message doesn't belong to Identifier")
	}

	return nil
}

func (c *Controller) InstanceForHeight(height qbftspec.Height) *instance.Instance {
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
func (c *Controller) addAndStoreNewInstance() *instance.Instance {
	i := instance.NewInstance(c.GetConfig(), c.Share, c.Identifier, c.Height)
	c.StoredInstances.addNewInstance(i)
	return i
}

func (c *Controller) canStartInstance(height qbftspec.Height, value []byte) error {
	if height > qbftspec.FirstHeight {
		// check prev instance if prev instance is not the first instance
		inst := c.StoredInstances.FindInstance(height - 1)
		if inst == nil {
			return errors.New("could not find previous instance")
		}
		if decided, _ := inst.IsDecided(); !decided {
			return errors.New("previous instance hasn't Decided")
		}
	}

	// check value
	if err := c.GetConfig().GetValueCheckF()(value); err != nil {
		return errors.Wrap(err, "value invalid")
	}

	return nil
}

// GetRoot returns the state's deterministic root
func (c *Controller) GetRoot() ([]byte, error) {
	rootStruct := struct {
		Identifier             []byte
		Height                 qbftspec.Height
		InstanceRoots          [][]byte
		HigherReceivedMessages map[types.OperatorID]qbftspec.Height
		Domain                 types.DomainType
		Share                  *types.Share
	}{
		Identifier:             c.Identifier,
		Height:                 c.Height,
		InstanceRoots:          make([][]byte, len(c.StoredInstances)),
		HigherReceivedMessages: c.FutureMsgsContainer,
		Domain:                 c.Domain,
		Share:                  c.Share,
	}

	for i, inst := range c.StoredInstances {
		if inst != nil {
			r, err := inst.GetRoot()
			if err != nil {
				return nil, errors.Wrap(err, "failed getting instance root")
			}
			rootStruct.InstanceRoots[i] = r
		}
	}

	marshaledRoot, err := json.Marshal(rootStruct)
	if err != nil {
		return nil, errors.Wrap(err, "could not encode state")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret[:], nil
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

	config := c.GetConfig()
	for _, i := range c.StoredInstances {
		if i != nil {
			//  TODO-spec-align rethink if we need it
			i.SetConfig(config)
		}
	}
	return nil
}

func (c *Controller) saveAndBroadcastDecided(aggregatedCommit *qbftspec.SignedMessage) error {
	if err := c.GetConfig().GetStorage().SaveHighestDecided(aggregatedCommit); err != nil {
		return errors.Wrap(err, "could not save decided")
	}

	// Broadcast Decided msg
	byts, err := aggregatedCommit.Encode()
	if err != nil {
		return errors.Wrap(err, "could not encode decided message")
	}

	msgToBroadcast := &types.SSVMessage{
		MsgType: types.SSVConsensusMsgType,
		MsgID:   qbftspec.ControllerIdToMessageID(c.Identifier),
		Data:    byts,
	}
	if err := c.GetConfig().GetNetwork().Broadcast(msgToBroadcast); err != nil {
		// We do not return error here, just Log broadcasting error.
		return errors.Wrap(err, "could not broadcast decided")
	}
	return nil
}

func (c *Controller) GetConfig() types2.IConfig {
	return c.config
}
