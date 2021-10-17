package storage

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
)

// ShareOptions - used to load validator share from config
type ShareOptions struct {
	NodeID    uint64         `yaml:"NodeID" env:"NodeID" env-description:"Local share node ID"`
	PublicKey string         `yaml:"PublicKey" env:"LOCAL_NODE_ID" env-description:"Local validator public key"`
	ShareKey  string         `yaml:"ShareKey" env:"LOCAL_SHARE_KEY" env-description:"Local share key"`
	Committee map[string]int `yaml:"Committee" env:"LOCAL_COMMITTEE" env-description:"Local validator committee array"`
}

// ToShare creates a Share instance from ShareOptions
func (options *ShareOptions) ToShare() (*Share, error) {
	var err error

	if len(options.PublicKey) > 0 && len(options.ShareKey) > 0 && len(options.Committee) > 0 {
		shareKey := &bls.SecretKey{}

		if err = shareKey.SetHexString(options.ShareKey); err != nil {
			return nil, errors.Wrap(err, "failed to set hex private key")
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
		ibftCommittee := make(map[uint64]*proto.Node)

		for pk, id := range options.Committee {
			ibftCommittee[uint64(id)] = &proto.Node{
				IbftId: uint64(id),
				Pk:     _getBytesFromHex(pk),
			}
			if uint64(id) == options.NodeID {
				ibftCommittee[options.NodeID].Pk = shareKey.GetPublicKey().Serialize()
			}
		}

		if err != nil {
			return nil, err
		}

		share := Share{
			NodeID:    options.NodeID,
			Metadata:  nil,
			PublicKey: validatorPk,
			Committee: ibftCommittee,
		}
		return &share, nil
	}
	return nil, errors.New("empty share")
}
