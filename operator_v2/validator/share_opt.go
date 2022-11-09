package validator

import (
	"encoding/hex"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"

	v1types "github.com/bloxapp/ssv/protocol/v1/types"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

// ShareOptions - used to load validator share from config
type ShareOptions struct {
	NodeID       uint64         `yaml:"NodeID" env:"NodeID" env-description:"Local share node ID"`
	PublicKey    string         `yaml:"PublicKey" env:"LOCAL_NODE_ID" env-description:"Local validator public key"`
	ShareKey     string         `yaml:"ShareKey" env:"LOCAL_SHARE_KEY" env-description:"Local share key"`
	Committee    map[string]int `yaml:"Committee" env:"LOCAL_COMMITTEE" env-description:"Local validator committee array"`
	OwnerAddress string         `yaml:"OwnerAddress" env:"LOCAL_OWNER_ADDRESS" env-description:"Local validator owner address"`
	Operators    []string       `yaml:"Operators" env:"LOCAL_OPERATORS" env-description:"Local validator selected operators"`
	OperatorIds  []int          `yaml:"OperatorIds" env:"LOCAL_OPERATOR_IDS" env-description:"Local validator selected operator ids"`
}

func (options *ShareOptions) ToShare() (*types.SSVShare, error) {
	if !options.valid() {
		return nil, errors.New("empty or invalid share")
	}

	var err error
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

	var sharePK []byte
	committee := make([]*spectypes.Operator, 0)
	for pkString, id := range options.Committee {
		pkBytes := _getBytesFromHex(pkString)
		committee = append(committee, &spectypes.Operator{
			OperatorID: spectypes.OperatorID(id),
			PubKey:     pkBytes,
		})

		// TODO: check if need to use options.ShareKey instead
		if spectypes.OperatorID(id) == spectypes.OperatorID(options.NodeID) {
			sharePK = pkBytes
		}
	}

	if err != nil {
		return nil, err
	}

	var operators [][]byte
	for _, op := range options.Operators {
		operators = append(operators, []byte(op))
	}

	var operatorIDs []uint64
	for _, opID := range options.OperatorIds {
		operatorIDs = append(operatorIDs, uint64(opID))
	}

	share := &types.SSVShare{
		Share: spectypes.Share{
			OperatorID:      spectypes.OperatorID(options.NodeID),
			ValidatorPubKey: validatorPk.Serialize(),
			SharePubKey:     sharePK,
			Committee:       committee,
			Quorum:          3,                          // temp
			PartialQuorum:   2,                          // temp
			DomainType:      v1types.GetDefaultDomain(), // temp
			Graffiti:        nil,
		},
		ShareMetadata: types.ShareMetadata{
			OwnerAddress: options.OwnerAddress,
			Operators:    operators,
			OperatorIDs:  operatorIDs,
		},
	}
	return share, nil

}

func (options *ShareOptions) valid() bool {
	return options != nil &&
		len(options.PublicKey) > 0 &&
		len(options.ShareKey) > 0 &&
		len(options.Committee) > 0 &&
		len(options.OwnerAddress) > 0 &&
		len(options.Operators) > 0 &&
		len(options.OperatorIds) > 0
}
