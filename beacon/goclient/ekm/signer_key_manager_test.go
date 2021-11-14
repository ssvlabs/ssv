package ekm

import (
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/utils/threshold"
	fssz "github.com/ferranbt/fastssz"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/go-ssz"
	"github.com/stretchr/testify/require"
	"testing"
)

const (
	sk1Str = "3548db63ab5701878daf25fa877638dc7809778815b9d9ecd5369da33ca9e64f"
	pk1Str = "a8cb269bd7741740cfe90de2f8db6ea35a9da443385155da0fa2f621ba80e5ac14b5c8f65d23fd9ccc170cc85f29e27d"
	sk2Str = "66dd37ae71b35c81022cdde98370e881cff896b689fa9136917f45afce43fd3b"
	pk2Str = "8796fafa576051372030a75c41caafea149e4368aebaca21c9f90d9974b3973d5cee7d7874e4ec9ec59fb2c8945b3e01"
)

type signingUtils struct {
}

func (s *signingUtils) GetDomain(data *spec.AttestationData) ([]byte, error) {
	return make([]byte, 32), nil
}

func (s *signingUtils) ComputeSigningRoot(object interface{}, domain []byte) ([32]byte, error) {
	if object == nil {
		return [32]byte{}, errors.New("cannot compute signing root of nil")
	}
	return s.signingData(func() ([32]byte, error) {
		if v, ok := object.(fssz.HashRoot); ok {
			return v.HashTreeRoot()
		}
		return ssz.HashTreeRoot(object)
	}, domain)
}

func (s *signingUtils) signingData(rootFunc func() ([32]byte, error), domain []byte) ([32]byte, error) {
	objRoot, err := rootFunc()
	if err != nil {
		return [32]byte{}, err
	}
	root := spec.Root{}
	copy(root[:], objRoot[:])
	_domain := spec.Domain{}
	copy(_domain[:], domain)
	container := &spec.SigningData{
		ObjectRoot: root,
		Domain:     _domain,
	}
	return container.HashTreeRoot()
}

func testKeyManager(t *testing.T) beacon.KeyManager {
	threshold.Init()

	km, err := NewETHKeyManagerSigner(getStorage(t), nil, core.PraterNetwork)
	km.(*ethKeyManagerSigner).signingUtils = &signingUtils{}
	require.NoError(t, err)

	sk1 := &bls.SecretKey{}
	require.NoError(t, sk1.SetHexString(sk1Str))

	sk2 := &bls.SecretKey{}
	require.NoError(t, sk2.SetHexString(sk2Str))

	require.NoError(t, km.AddShare(sk1))
	require.NoError(t, km.AddShare(sk2))

	return km
}

func TestSignIBFTMessage(t *testing.T) {
	km := testKeyManager(t)

	t.Run("pk 1", func(t *testing.T) {
		pk := &bls.PublicKey{}
		require.NoError(t, pk.Deserialize(_byteArray(pk1Str)))

		msg := &proto.Message{
			Type:      proto.RoundState_Commit,
			Round:     2,
			Lambda:    []byte("lambda1"),
			SeqNumber: 3,
			Value:     []byte("value1"),
		}

		// sign
		sig, err := km.SignIBFTMessage(msg, pk.Serialize())
		require.NoError(t, err)

		// verify
		signed := &proto.SignedMessage{
			Message:   msg,
			Signature: sig,
			SignerIds: []uint64{1},
		}
		res, err := signed.VerifySig(pk)
		require.NoError(t, err)
		require.True(t, res)
	})

	t.Run("pk 2", func(t *testing.T) {
		pk := &bls.PublicKey{}
		require.NoError(t, pk.Deserialize(_byteArray(pk2Str)))

		msg := &proto.Message{
			Type:      proto.RoundState_ChangeRound,
			Round:     3,
			Lambda:    []byte("lambda2"),
			SeqNumber: 1,
			Value:     []byte("value2"),
		}

		// sign
		sig, err := km.SignIBFTMessage(msg, pk.Serialize())
		require.NoError(t, err)

		// verify
		signed := &proto.SignedMessage{
			Message:   msg,
			Signature: sig,
			SignerIds: []uint64{1},
		}
		res, err := signed.VerifySig(pk)
		require.NoError(t, err)
		require.True(t, res)
	})
}
