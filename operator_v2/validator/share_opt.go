package validator

import (
	"encoding/hex"
	"sort"

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
	Committee    map[string]int `yaml:"Committee" env:"LOCAL_COMMITTEE" env-description:"Local validator committee array"`
	OwnerAddress string         `yaml:"OwnerAddress" env:"LOCAL_OWNER_ADDRESS" env-description:"Local validator owner address"`
	Operators    []string       `yaml:"Operators" env:"LOCAL_OPERATORS" env-description:"Local validator selected operators"`
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

	sort.Slice(committee, func(i, j int) bool {
		return committee[i].OperatorID < committee[j].OperatorID
	})

	f := uint64(len(committee)-1) / 3
	share := &types.SSVShare{
		Share: spectypes.Share{
			OperatorID:      spectypes.OperatorID(options.NodeID),
			ValidatorPubKey: validatorPk.Serialize(),
			SharePubKey:     sharePK,
			Committee:       committee,
			Quorum:          3 * f,
			PartialQuorum:   2 * f,
			DomainType:      v1types.GetDefaultDomain(), // temp; TODO: decide about the value
			Graffiti:        nil,
		},
		Metadata: types.Metadata{
			OwnerAddress: options.OwnerAddress,
			Operators:    operators,
		},
	}

	return share, nil
}

func (options *ShareOptions) valid() bool {
	return options != nil &&
		len(options.PublicKey) > 0 &&
		len(options.Committee) > 0 &&
		len(options.OwnerAddress) > 0 &&
		len(options.Operators) > 0
}
