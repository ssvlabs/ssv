package kms

import (
	"encoding/json"
	"fmt"

	dkgkms "github.com/ssvlabs/ssv-dkg/pkgs/kms"

	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/storage/basedb"
)

var (
	prefixKey     = []byte("kms/key/")
	prefixState   = []byte("kms/state/")
	prefixPresig  = []byte("kms/presig/")
	prefixResults = []byte("kms/result/")
)

// store persists dKMS state in the node's database. Secret material (shares stay encrypted to the
// operator's RSA key as in the key file; presignatures are encrypted with the operator's storage
// key, like the node's other key material).
type store struct {
	db     basedb.Database
	encKey []byte
}

func newStore(db basedb.Database, operatorKey keys.OperatorPrivateKey) (*store, error) {
	encKey, err := operatorKey.EKMEncryptionKey()
	if err != nil {
		return nil, fmt.Errorf("derive storage key: %w", err)
	}
	return &store{db: db, encKey: encKey}, nil
}

func (s *store) saveKeyFile(kf *dkgkms.KeyFile) error {
	b, err := json.Marshal(kf)
	if err != nil {
		return err
	}
	return s.db.Set(prefixKey, []byte(kf.KeyID), b)
}

func (s *store) keyFiles() ([]*dkgkms.KeyFile, error) {
	var out []*dkgkms.KeyFile
	err := s.db.GetAll(prefixKey, func(_ int, obj basedb.Obj) error {
		kf := &dkgkms.KeyFile{}
		if err := json.Unmarshal(obj.Value, kf); err != nil {
			return err
		}
		out = append(out, kf)
		return nil
	})
	return out, err
}

// runnerState is the part of a key's replicated log the runner needs after a restart.
type runnerState struct {
	// Height is the next QBFT height (last decided + 1).
	Height uint64 `json:"height"`
	// Decided maps decided request IDs to their height.
	Decided map[string]uint64 `json:"decided"`
	// Consumed lists presignature IDs used by decided requests: never sign with them again.
	Consumed map[string]bool `json:"consumed"`
	// Nonces maps transaction sequences already signed ("<chain>/<nonce>") to their request ID.
	Nonces map[string]string `json:"nonces"`
}

func (s *store) loadState(keyID string) (*runnerState, error) {
	st := &runnerState{Decided: map[string]uint64{}, Consumed: map[string]bool{}, Nonces: map[string]string{}}
	obj, found, err := s.db.Get(prefixState, []byte(keyID))
	if err != nil || !found {
		return st, err
	}
	if err := json.Unmarshal(obj.Value, st); err != nil {
		return nil, err
	}
	return st, nil
}

func (s *store) saveState(keyID string, st *runnerState) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return s.db.Set(prefixState, []byte(keyID), b)
}

// storedPresig is a presignature at rest.
type storedPresig struct {
	ID         string   `json:"id"`
	Signers    []uint64 `json:"signers"`
	Commitment []byte   `json:"commitment"`
	Sealed     []byte   `json:"sealed"`
}

func presigKey(keyID, id string) []byte { return []byte(keyID + "/" + id) }

func (s *store) savePresig(keyID string, sp *storedPresig, plain []byte) error {
	sealed, err := keys.EncryptPayload(s.encKey, plain)
	if err != nil {
		return err
	}
	sp.Sealed = sealed
	b, err := json.Marshal(sp)
	if err != nil {
		return err
	}
	return s.db.Set(prefixPresig, presigKey(keyID, sp.ID), b)
}

func (s *store) openPresig(sp *storedPresig) ([]byte, error) {
	return keys.DecryptPayload(s.encKey, sp.Sealed)
}

func (s *store) deletePresig(keyID, id string) error {
	return s.db.Delete(prefixPresig, presigKey(keyID, id))
}

func (s *store) presigs(keyID string) ([]*storedPresig, error) {
	var out []*storedPresig
	err := s.db.GetAll(append(append([]byte{}, prefixPresig...), []byte(keyID+"/")...), func(_ int, obj basedb.Obj) error {
		sp := &storedPresig{}
		if err := json.Unmarshal(obj.Value, sp); err != nil {
			return err
		}
		out = append(out, sp)
		return nil
	})
	return out, err
}

func (s *store) saveResult(r *Result) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return s.db.Set(prefixResults, []byte(r.ID), b)
}

func (s *store) result(id string) (*Result, bool, error) {
	obj, found, err := s.db.Get(prefixResults, []byte(id))
	if err != nil || !found {
		return nil, false, err
	}
	r := &Result{}
	if err := json.Unmarshal(obj.Value, r); err != nil {
		return nil, false, err
	}
	return r, true, nil
}
