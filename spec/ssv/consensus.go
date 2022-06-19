package ssv

import (
	"bytes"
	"github.com/bloxapp/ssv/spec/qbft"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/pkg/errors"
)

// Decide starts a new consensus instance for input value
func (dr *Runner) Decide(input *types.ConsensusData) error {
	byts, err := input.Encode()
	if err != nil {
		return errors.Wrap(err, "could not encode ConsensusData")
	}

	if err := dr.valCheck(byts); err != nil {
		return errors.Wrap(err, "input data invalid")
	}

	if err := dr.QBFTController.StartNewInstance(byts); err != nil {
		return errors.Wrap(err, "could not start new QBFT instance")
	}
	newInstance := dr.QBFTController.InstanceForHeight(dr.QBFTController.Height)
	if newInstance == nil {
		return errors.New("could not find newly created QBFT instance")
	}

	dr.State.RunningInstance = newInstance
	return nil
}

func (dr *Runner) ProcessConsensusMessage(msg *qbft.SignedMessage) (decided bool, decidedValue *types.ConsensusData, err error) {
	decided, decidedValueByts, err := dr.QBFTController.ProcessMsg(msg)
	if err != nil {
		return false, nil, errors.Wrap(err, "failed to process consensus msg")
	}

	/**
	Decided returns true only once so if it is true it must be for the current running instance
	*/
	if !decided {
		return false, nil, nil
	}

	decidedValue = &types.ConsensusData{}
	if err := decidedValue.Decode(decidedValueByts); err != nil {
		return true, nil, errors.Wrap(err, "failed to parse decided value to ConsensusData")
	}

	if err := dr.validateDecidedConsensusData(decidedValue); err != nil {
		return true, nil, errors.Wrap(err, "decided ConsensusData invalid")
	}

	return true, decidedValue, nil
}

func (dr *Runner) validateDecidedConsensusData(val *types.ConsensusData) error {
	byts, err := val.Encode()
	if err != nil {
		return errors.Wrap(err, "could not encode decided value")
	}
	if err := dr.valCheck(byts); err != nil {
		return errors.Wrap(err, "decided value is invalid")
	}

	if dr.BeaconRoleType != val.Duty.Type {
		return errors.New("decided value's duty has wrong beacon role type")
	}

	if !bytes.Equal(dr.Share.ValidatorPubKey, val.Duty.PubKey[:]) {
		return errors.New("decided value's validator pk is wrong")
	}

	return nil
}
