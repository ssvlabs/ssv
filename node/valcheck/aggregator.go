package valcheck

import (
	"encoding/json"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/pkg/errors"
)

// AggregatorValueCheck checks for an Aggregator type value
type AggregatorValueCheck struct {
}

// Check returns error if value is invalid
func (v *AggregatorValueCheck) Check(value []byte) error {
	// try and parse to attestation data
	inputValue := &proto.InputValue_Aggregation{}
	if err := json.Unmarshal(value, &inputValue); err != nil {
		return errors.Wrap(err, "could not parse input value storing attestation data")
	}

	//if inputValue.Aggregation.Message.Aggregate.Data.Slot == 0 {
	//	return errors.New("this is an example test error")
	//}

	// TODO - test for slashing protection
	return nil
}
