package spectesting

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/beacon"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"testing"

	"github.com/bloxapp/ssv/ibft/proto"
)

// SignMsg signs the given message by the given private key
func SignMsg(t *testing.T, id uint64, sk *bls.SecretKey, msg *proto.Message) *proto.SignedMessage {
	require.NoError(t, bls.Init(bls.BLS12_381))

	signature, err := msg.Sign(sk)
	require.NoError(t, err)
	return &proto.SignedMessage{
		Message:   msg,
		Signature: signature.Serialize(),
		SignerIds: []uint64{id},
	}
}

type testKM struct {
	keys map[string]*bls.SecretKey
}

func newTestKM() beacon.KeyManager {
	return &testKM{
		keys: make(map[string]*bls.SecretKey),
	}
}

func (km *testKM) AddShare(shareKey *bls.SecretKey) error {
	if km.getKey(shareKey.GetPublicKey()) == nil {
		km.keys[shareKey.GetPublicKey().SerializeToHexStr()] = shareKey
	}
	return nil
}

func (km *testKM) getKey(key *bls.PublicKey) *bls.SecretKey {
	return km.keys[key.SerializeToHexStr()]
}

func (km *testKM) SignIBFTMessage(message *proto.Message, pk []byte) ([]byte, error) {
	if key := km.keys[hex.EncodeToString(pk)]; key != nil {
		sig, err := message.Sign(key)
		if err != nil {
			return nil, errors.Wrap(err, "could not sign ibft msg")
		}
		return sig.Serialize(), nil
	}
	return nil, errors.New("could not find key for pk")
}
