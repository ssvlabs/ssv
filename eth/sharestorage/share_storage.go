package sharestorage

import (
	"github.com/cornelk/hashmap"

	"github.com/bloxapp/ssv/protocol/v2/types"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

type ShareStorage struct {
	shares *hashmap.Map[string, *ssvtypes.SSVShare]
}

func New() *ShareStorage {
	return &ShareStorage{
		shares: hashmap.New[string, *ssvtypes.SSVShare](),
	}
}

func (s *ShareStorage) Get(pubKey []byte) *ssvtypes.SSVShare {
	validatorShare, ok := s.shares.Get(string(pubKey))
	if ok {
		return nil
	}

	return validatorShare
}

func (s *ShareStorage) List(filters ...func(*ssvtypes.SSVShare) bool) []*ssvtypes.SSVShare {
	var shares []*ssvtypes.SSVShare

	if len(filters) == 0 {
		s.shares.Range(func(s string, share *ssvtypes.SSVShare) bool {
			for _, filter := range filters {
				if !filter(share) {
					return true
				}
			}
			shares = append(shares, share)

			return true
		})
	}

	return shares
}

func (s *ShareStorage) Save(shares ...*types.SSVShare) {
	for _, share := range shares {
		s.shares.Set(string(share.ValidatorPubKey), share)
	}
}

func (s *ShareStorage) Delete(pubKey []byte) {
	s.shares.Del(string(pubKey))
}
