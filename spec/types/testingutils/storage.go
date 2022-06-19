package testingutils

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/spec/qbft"
)

type testingStorage struct {
	storage map[string]*qbft.SignedMessage
}

func NewTestingStorage() *testingStorage {
	return &testingStorage{
		storage: make(map[string]*qbft.SignedMessage),
	}
}

// SaveHighestDecided saves the Decided value as highest for a validator PK and role
func (s *testingStorage) SaveHighestDecided(signedMsg *qbft.SignedMessage) error {
	s.storage[hex.EncodeToString(signedMsg.Message.Identifier)] = signedMsg
	return nil
}

//// GetHighestDecided returns the saved Decided value (highest) for a validator PK and role
//func (s *testingStorage) GetHighestDecided(validatorPK []byte, role beacon.RoleType) (*consensusData, error) {
//	if s.storage[hex.EncodeToString(validatorPK)] == nil {
//		return nil, errors.New("can't find validator PK")
//	}
//	if value, found := s.storage[hex.EncodeToString(validatorPK)][role]; found {
//		return value, nil
//	}
//	return s.storage[hex.EncodeToString(signedMsg.Message.Identifier)], errors.New("can't find role")
//}
