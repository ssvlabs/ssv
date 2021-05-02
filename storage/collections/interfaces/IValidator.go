package interfaces

import (
	"bytes"
	"encoding/gob"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/storage"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
)

// IValidator interface for validator storage
type IValidator interface {
	LoadFromConfig(validator *Validator)
	SaveValidatorShare(validator *Validator) error
	GetAllValidatorsShare() ([]*Validator, error)
}

// Validator for ValidatorStorage struct creation
type Validator struct {
	PubKey     *bls.PublicKey
	ShareKey   *bls.SecretKey
	Committiee map[uint64]*proto.Node
}

//  serializedValidator struct
type serializedValidator struct {
	ShareKey   []byte
	Committiee map[uint64]*proto.Node
}

// Serialize Validator to []byte for db purposes
func (v *Validator) Serialize() ([]byte, error) {
	value := serializedValidator{
		ShareKey:   v.ShareKey.Serialize(),
		Committiee: v.Committiee,
	}

	var b bytes.Buffer
	e := gob.NewEncoder(&b)
	if err := e.Encode(value); err != nil {
		return nil, errors.Wrap(err, "Failed to encode serializedValidator")
	}
	return b.Bytes(), nil
}

// Deserialize key/value to Validator struct
func (v *Validator) Deserialize(obj storage.Obj) (*Validator, error) {
	var valShare serializedValidator
	d := gob.NewDecoder(bytes.NewReader(obj.Value))
	if err := d.Decode(&valShare); err != nil {
		return nil, errors.Wrap(err, "Failed to get val value")
	}
	shareSecret := &bls.SecretKey{} // need to decode secret separately cause of encoding has private var limit in bls.SecretKey struct
	if err := shareSecret.Deserialize(valShare.ShareKey); err != nil {
		return nil, errors.Wrap(err, "Failed to get key secret")
	}
	pubKey := &bls.PublicKey{}
	if err := pubKey.Deserialize(obj.Key); err != nil {
		return nil, errors.Wrap(err, "Failed to get pubkey")
	}
	return &Validator{
		PubKey:     pubKey,
		ShareKey:   shareSecret,
		Committiee: valShare.Committiee,
	}, nil
}