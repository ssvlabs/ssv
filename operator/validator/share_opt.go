package validator

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
)

// ShareOptions - used to load validator share from config
type ShareOptions struct {
	NodeID       uint64         `yaml:"NodeID" env:"NodeID" env-description:"Local share node ID"`
	PublicKey    string         `yaml:"PublicKey" env:"LOCAL_NODE_ID" env-description:"Local validator public key"`
	ShareKey     string         `yaml:"ShareKey" env:"LOCAL_SHARE_KEY" env-description:"Local share key"`
	Committee    map[string]int `yaml:"Committee" env:"LOCAL_COMMITTEE" env-description:"Local validator committee array"`
	OwnerAddress string         `yaml:"OwnerAddress" env:"LOCAL_OWNER_ADDRESS" env-description:"Local validator owner address"`
	Operators    []string       `yaml:"Operators" env:"LOCAL_OPERATORS" env-description:"Local validator selected operators"`
}

// ToShare creates a Share instance from ShareOptions
func (options *ShareOptions) ToShare() (*beacon.Share, error) {
	var err error

	if len(options.PublicKey) > 0 && len(options.ShareKey) > 0 && len(options.Committee) > 0 && len(options.OwnerAddress) > 0 {
		validatorPk := &bls.PublicKey{}
		if err = validatorPk.DeserializeHexStr(options.PublicKey); err != nil {
			return nil, errors.Wrap(err, "failed to decode validator key")
		}

		_getBytesFromHex := func(str string) []byte {
			val, e := hex.DecodeString(str)
			if e != nil {
				err = errors.Wrap(err, "failed to decode committee")
			}
			return val
		}
		ibftCommittee := make(map[message.OperatorID]*beacon.Node)
		for pk, id := range options.Committee {
			ibftCommittee[message.OperatorID(id)] = &beacon.Node{
				IbftID: uint64(id),
				Pk:     _getBytesFromHex(pk),
			}
		}

		var operators [][]byte
		for _, op := range options.Operators {
			operators = append(operators, []byte(op))
		}

		if err != nil {
			return nil, err
		}

		share := beacon.Share{
			NodeID:       message.OperatorID(options.NodeID),
			Metadata:     nil,
			PublicKey:    validatorPk,
			Committee:    ibftCommittee,
			OwnerAddress: options.OwnerAddress,
			Operators:    operators,
		}
		return &share, nil
	}
	return nil, errors.New("empty share")
}
