package testingutils

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/spec/dkg"
	"github.com/bloxapp/ssv/spec/qbft"
	"github.com/bloxapp/ssv/spec/types"
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

// GetDKGOperator returns true and operator object if found by operator ID
func (s *testingStorage) GetDKGOperator(operatorID types.OperatorID) (bool, *dkg.Operator, error) {
	panic("implemnent")
}
