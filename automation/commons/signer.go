package commons

import (
	"encoding/hex"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	v0 "github.com/bloxapp/ssv/protocol/v1/qbft/instance/forks/v0"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"sync"
)

type testSigner struct {
	lock sync.Locker
	keys map[string]*bls.SecretKey
}

// NewTestSigner creates a new signer for tests
func NewTestSigner() beacon.KeyManager {
	return &testSigner{&sync.Mutex{}, make(map[string]*bls.SecretKey)}
}

func (km *testSigner) AddShare(shareKey *bls.SecretKey) error {
	km.lock.Lock()
	defer km.lock.Unlock()

	if km.getKey(shareKey.GetPublicKey()) == nil {
		km.keys[shareKey.GetPublicKey().SerializeToHexStr()] = shareKey
	}
	return nil
}

func (km *testSigner) getKey(key *bls.PublicKey) *bls.SecretKey {
	return km.keys[key.SerializeToHexStr()]
}

func (km *testSigner) SignIBFTMessage(message *message.ConsensusMessage, pk []byte, forkVersion string) ([]byte, error) {
	km.lock.Lock()
	defer km.lock.Unlock()

	if key := km.keys[hex.EncodeToString(pk)]; key != nil {
		sig, err := message.Sign(key, (&v0.ForkV0{}).VersionName()) // TODO need to check fork v1?
		if err != nil {
			return nil, errors.Wrap(err, "could not sign ibft msg")
		}
		return sig.Serialize(), nil
	}
	return nil, errors.New("could not find key for pk")
}

func (km *testSigner) SignAttestation(data *spec.AttestationData, duty *beacon.Duty, pk []byte) (*spec.Attestation, []byte, error) {
	return nil, nil, nil
}
