package valcheck

import (
	"encoding/json"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/pkg/errors"
)

type AttestationValueCheck struct {
}

func (v *AttestationValueCheck) Check(value []byte) error {
	// try and parse to attestation data
	inputValue := &proto.InputValue_AttestationData{}
	if err := json.Unmarshal(value, &inputValue); err != nil {
		return errors.Wrap(err, "could not parse input value storing attestation data")
	}

	if inputValue.AttestationData.Slot == 0 {
		return errors.New("this is an example test error")
	}

	// TODO - test for slashing protection
	return nil
}
