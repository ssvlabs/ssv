package dkg

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/pkg/errors"
)

// Runners is a map of dkg runners mapped by dkg ID.
type Runners map[string]*Runner

func (runners Runners) AddRunner(id RequestID, runner *Runner) {
	runners[hex.EncodeToString(id[:])] = runner
}

// RunnerForID returns a Runner from the provided msg ID, or nil if not found
func (runners Runners) RunnerForID(id RequestID) *Runner {
	return runners[hex.EncodeToString(id[:])]
}

func (runners Runners) DeleteRunner(id RequestID) {
	delete(runners, hex.EncodeToString(id[:]))
}

type Node struct {
	operator *Operator
	// runners holds all active running DKG runners
	runners Runners
	config  *Config
}

func NewNode(operator *Operator, config *Config) *Node {
	return &Node{
		operator: operator,
		config:   config,
		runners:  make(Runners, 0),
	}
}

func (n *Node) newRunner(id RequestID, initMsg *Init) (*Runner, error) {
	runner := &Runner{
		Operator:              n.operator,
		DepositDataSignatures: map[types.OperatorID]*PartialDepositData{},
		config:                n.config,
		protocol:              n.config.Protocol(n.config.Network, n.operator.OperatorID, id),
	}

	if err := runner.protocol.Start(initMsg); err != nil {
		return nil, errors.Wrap(err, "could not start dkg protocol")
	}

	return runner, nil
}

// ProcessMessage processes network Messages of all types
func (n *Node) ProcessMessage(msg *types.SSVMessage) error {
	// TODO validate msg

	signedMsg := &SignedMessage{}
	if err := signedMsg.Decode(msg.GetData()); err != nil {
		return errors.Wrap(err, "could not get dkg Message from network Messages")
	}

	switch signedMsg.Message.MsgType {
	case InitMsgType:
		return n.startNewDKGMsg(signedMsg)
	case ProtocolMsgType:
		return n.processDKGMsg(signedMsg)
	default:
		return errors.New("unknown msg type")
	}
}

func (n *Node) startNewDKGMsg(message *SignedMessage) error {
	initMsg, err := n.validateInitMsg(message)
	if err != nil {
		return errors.Wrap(err, "could not process new dkg msg")
	}

	runner, err := n.newRunner(message.Message.Identifier, initMsg)
	if err != nil {
		return errors.Wrap(err, "could not start new dkg")
	}

	// add runner to runners
	n.runners.AddRunner(message.Message.Identifier, runner)

	return nil
}

func (n *Node) validateInitMsg(message *SignedMessage) (*Init, error) {
	if err := message.Validate(); err != nil {
		return nil, errors.Wrap(err, "message invalid")
	}

	// validate identifier.GetEthAddress is the signer for message
	if err := message.Signature.ECRecover(message, n.config.SignatureDomainType, types.DKGSignatureType, message.Message.Identifier.GetETHAddress()); err != nil {
		return nil, errors.Wrap(err, "signed message invalid")
	}

	initMsg := &Init{}
	if err := initMsg.Decode(message.Message.Data); err != nil {
		return nil, errors.Wrap(err, "could not get dkg init Message from signed Messages")
	}

	if err := initMsg.Validate(); err != nil {
		return nil, errors.Wrap(err, "init message invalid")
	}

	// check instance not running already
	if n.runners.RunnerForID(message.Message.Identifier) != nil {
		return nil, errors.New("dkg started already")
	}

	return initMsg, nil
}

func (n *Node) processDKGMsg(message *SignedMessage) error {
	runner := n.runners.RunnerForID(message.Message.Identifier)
	if runner == nil {
		return errors.New("could not find dkg runner")
	}

	finished, output, err := runner.ProcessMsg(message)
	if err != nil {
		return errors.Wrap(err, "could not process dkg message")
	}

	if finished {
		if err := n.config.Network.StreamDKGOutput(output); err != nil {
			return errors.Wrap(err, "failed to stream dkg output")
		}
		n.runners.DeleteRunner(message.Message.Identifier)
	}

	return nil
}
