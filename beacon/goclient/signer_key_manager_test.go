package goclient

import (
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/utils/threshold"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"testing"
)

const (
	sk1Str = "3548db63ab5701878daf25fa877638dc7809778815b9d9ecd5369da33ca9e64f"
	pk1Str = "a8cb269bd7741740cfe90de2f8db6ea35a9da443385155da0fa2f621ba80e5ac14b5c8f65d23fd9ccc170cc85f29e27d"
	sk2Str = "66dd37ae71b35c81022cdde98370e881cff896b689fa9136917f45afce43fd3b"
	pk2Str = "8796fafa576051372030a75c41caafea149e4368aebaca21c9f90d9974b3973d5cee7d7874e4ec9ec59fb2c8945b3e01"
)

func testBeaconSigner(t *testing.T) beacon.Beacon {
	threshold.Init()

	signerWallet, storage, err := openOrCreateWallet(getStorage(t), core.PraterNetwork)
	require.NoError(t, err)
	signer, err := newBeaconSigner(signerWallet, storage, core.PraterNetwork)
	require.NoError(t, err)

	ret := &goClient{
		wallet: signerWallet,
		signer: signer,
	}

	sk1 := &bls.SecretKey{}
	require.NoError(t, sk1.SetHexString(sk1Str))

	sk2 := &bls.SecretKey{}
	require.NoError(t, sk2.SetHexString(sk2Str))

	require.NoError(t, ret.AddShare(sk1))
	require.NoError(t, ret.AddShare(sk2))

	return ret
}

func TestGoClient_SignIBFTMessage(t *testing.T) {
	gocli := testBeaconSigner(t)

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
		sig, err := gocli.SignIBFTMessage(msg, pk.Serialize())
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
		sig, err := gocli.SignIBFTMessage(msg, pk.Serialize())
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
