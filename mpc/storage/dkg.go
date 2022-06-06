package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"fmt"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/pkg/errors"
	"math/big"
)

type DkgRequest struct {
	Id                    big.Int
	OwnerAddress          string
	Operators             [][]byte
	WithdrawalCredentials []byte
	NodeID                uint64
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

// HashOperators hash all Operators keys key
func (r *DkgRequest) HashOperators() []string {
	hashes := make([]string, len(r.Operators))
	for i, o := range r.Operators {
		hashes[i] = fmt.Sprintf("%x", sha256.Sum256(o))
	}
	return hashes
}
