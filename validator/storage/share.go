package storage

import (
	"bytes"
	"encoding/gob"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
)

// Share storage model
type Share struct {
	NodeID    uint64
	PublicKey *bls.PublicKey
	ShareKey  *bls.SecretKey
	Committee map[uint64]*proto.Node
}

//  serializedShare struct
type serializedShare struct {
	NodeID    uint64
	ShareKey  []byte
	Committee map[uint64]*proto.Node
}

// Serialize share to []byte
func (s *Share) Serialize() ([]byte, error) {
	value := serializedShare{
		NodeID:    s.NodeID,
		ShareKey:  s.ShareKey.Serialize(),
		Committee: s.Committee,
	}

	var b bytes.Buffer
	e := gob.NewEncoder(&b)
	if err := e.Encode(value); err != nil {
		return nil, errors.Wrap(err, "Failed to encode serializedValidator")
	}
	return b.Bytes(), nil
}

// Deserialize key/value to Share model
func (s *Share) Deserialize(obj basedb.Obj) (*Share, error) {
	value := serializedShare{}
	d := gob.NewDecoder(bytes.NewReader(obj.Value))
	if err := d.Decode(&value); err != nil {
		return nil, errors.Wrap(err, "Failed to get val value")
	}
	shareSecret := &bls.SecretKey{} // need to decode secret separately cause of encoding has private var limit in bls.SecretKey struct
	// in exporter scenario, share key should be nil
	if value.ShareKey != nil && len(value.ShareKey) > 0 {
		if err := shareSecret.Deserialize(value.ShareKey); err != nil {
			return nil, errors.Wrap(err, "Failed to get key secret")
		}
	}
	pubKey := &bls.PublicKey{}
	if err := pubKey.Deserialize(obj.Key); err != nil {
		return nil, errors.Wrap(err, "Failed to get pubkey")
	}
	return &Share{
		NodeID:    value.NodeID,
		PublicKey: pubKey,
		ShareKey:  shareSecret,
		Committee: value.Committee,
	}, nil
}
