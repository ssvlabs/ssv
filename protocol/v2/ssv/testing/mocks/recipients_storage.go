package mocks

import (
	"github.com/ethereum/go-ethereum/common"

	"github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
)

type MockrecipientsStorage struct {
	FeeRecipient [20]byte
}

func NewMockrecipientsStorage() *MockrecipientsStorage {
	return &MockrecipientsStorage{}
}

func (m *MockrecipientsStorage) GetRecipientData(r basedb.Reader, owner common.Address) (*storage.RecipientData, bool, error) {
	return &storage.RecipientData{
		Owner:        common.Address{},
		FeeRecipient: m.FeeRecipient,
		Nonce:        nil,
	}, true, nil
}
