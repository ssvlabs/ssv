package weekday

import (
	"bytes"
	"errors"
	"time"

	"github.com/herumi/bls-eth-go-binary/bls"

	"github.com/bloxapp/ssv/ibft/valueImpl"
)

// weekdayConsensus implements valueImpl.ValueImplementation interface
type weekdayConsensus struct {
	weekday string
}

// New is the constructor of weekdayConsensus
func New() valueImpl.ValueImplementation {
	return &weekdayConsensus{
		weekday: time.Now().Weekday().String(),
	}
}

func (c *weekdayConsensus) ValidateValue(value []byte) error {
	if !bytes.Equal(value, []byte(c.weekday)) {
		return errors.New("msg value is wrong")
	}

	return nil
}

func (c *weekdayConsensus) SignValue(value []byte, skByts []byte) ([]byte, error) {
	sk := &bls.SecretKey{}
	if err := sk.Deserialize(skByts); err != nil {
		return nil, err
	}

	return sk.SignByte(value).Serialize(), nil
}
