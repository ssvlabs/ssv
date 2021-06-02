package valcheck

import (
	"encoding/json"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/pkg/errors"
)

// ProposerValueCheck checks for an Aggregator type value
type ProposerValueCheck struct {
}

// Check returns error if value is invalid
func (v *ProposerValueCheck) Check(value []byte) error {
	// try and parse to attestation data
	inputValue := &proto.InputValue_Block{}
	if err := json.Unmarshal(value, &inputValue); err != nil {
		return errors.Wrap(err, "could not parse input value storing attestation data")
	}

	//if inputValue.Block.Block.Slot == 0 {
	//	return errors.New("this is an example test error")
	//}

	// TODO - test for slashing protection
	return nil
}
