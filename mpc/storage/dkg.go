package storage

import (
	"bytes"
	"encoding/gob"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/pkg/errors"
)

type DkgRequest struct {
	Id uint64
	OwnerAddress string
	Operators    [][]byte
	WithdrawalCredentials []byte
}

// Serialize share to []byte
func (r *DkgRequest) Serialize() ([]byte, error) {
	var b bytes.Buffer
	e := gob.NewEncoder(&b)
	if err := e.Encode(r); err != nil {
		return nil, errors.Wrap(err, "Failed to encode DkgRequest")
	}
	return b.Bytes(), nil
}

// Deserialize key/value to DkgRequest model
func (r *DkgRequest) Deserialize(obj basedb.Obj) (*DkgRequest, error) {
	value := DkgRequest{}
	d := gob.NewDecoder(bytes.NewReader(obj.Value))
	if err := d.Decode(&value); err != nil {
		return nil, errors.Wrap(err, "Failed to get val value")
	}
	return &value, nil
}
