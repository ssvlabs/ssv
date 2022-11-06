package validator

import (
	"encoding/hex"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"

	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	typesv1 "github.com/bloxapp/ssv/protocol/v1/types"
	"github.com/bloxapp/ssv/protocol/v2/sharemetadata"
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

func (options *ShareOptions) valid() bool {
	return options != nil &&
		len(options.PublicKey) > 0 &&
		len(options.ShareKey) > 0 &&
		len(options.Committee) > 0 &&
		len(options.OwnerAddress) > 0 &&
		len(options.Operators) > 0 &&
		len(options.OperatorIds) > 0
}

// CreateShare creates a Share instance from ShareOptions
func (options *ShareOptions) CreateShare() (*spectypes.Share, error) {
	var err error

	if !options.valid() {
		return nil, errors.New("empty or invalid share")
	}

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

	s := &spectypes.Share{
		OperatorID:      spectypes.OperatorID(options.NodeID),
		ValidatorPubKey: validatorPk.Serialize(),
		SharePubKey:     sharePK,
		Committee:       committee,
		Quorum:          3,                          // temp
		PartialQuorum:   2,                          // temp
		DomainType:      typesv1.GetDefaultDomain(), // temp
		Graffiti:        nil,
	}
	return s, nil

}

// CreateMetadata creates a ShareMetadata instance from ShareOptions
func (options *ShareOptions) CreateMetadata() (*sharemetadata.ShareMetadata, error) {
	var err error

	if !options.valid() {
		return nil, errors.New("empty or invalid share")
	}
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
	ibftCommittee := make(map[spectypes.OperatorID]*beacon.Node)
	for pk, id := range options.Committee {
		ibftCommittee[spectypes.OperatorID(id)] = &beacon.Node{
			IbftID: uint64(id),
			Pk:     _getBytesFromHex(pk),
		}
	}

	var operators [][]byte
	for _, op := range options.Operators {
		operators = append(operators, []byte(op))
	}

	var operatorIDs []uint64
	for _, opID := range options.OperatorIds {
		operatorIDs = append(operatorIDs, uint64(opID))
	}

	if err != nil {
		return nil, err
	}

	metadata := &sharemetadata.ShareMetadata{
		PublicKey:    validatorPk,
		OwnerAddress: options.OwnerAddress,
		Operators:    operators,
		OperatorIDs:  operatorIDs,
	}
	return metadata, nil
}
