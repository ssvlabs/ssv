package beacon

import (
	"encoding/hex"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/pkg/errors"
	"sync"
)

type MockSigner interface {
	Signer
}

func NewMockSigner(sign mockSignFunc) MockSigner {
	return &mockSigner{
		lock: &sync.Mutex{},
		sign: sign,
	}
}

type mockSignFunc func(string, []byte) ([]byte, error)

type mockSigner struct {
	lock sync.Locker

	sign mockSignFunc
}

func (m *mockSigner) SignIBFTMessage(message *message.ConsensusMessage, pk []byte) ([]byte, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	root, err := message.GetRoot()
	if err != nil {
		return nil, errors.Wrap(err, "could not get message signing root")
	}

	sig, err := m.sign(hex.EncodeToString(pk), root)
	if err != nil {
		return nil, errors.Wrap(err, "could not sign message")
	}

	return sig, nil
}

func (m *mockSigner) SignAttestation(data *spec.AttestationData, duty *Duty, pk []byte) (*spec.Attestation, []byte, error) {
	panic("implement me")
}


