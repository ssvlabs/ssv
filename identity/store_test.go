package p2p

import (
	"encoding/hex"
	"testing"

	gcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/observability/log"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

var (
	sk  = "ba03f90c6e2e6d67e4a4682621412ddbafeb6bffdc169df8f2bd31f193f001d4"
	sk2 = "2340652c367bf8d17de1bc0454e6aa73e2eedd4a51686887d98d6b8813e5fb4a"
)

func TestSetupPrivateKey(t *testing.T) {
	logger := log.TestLogger(t)

	tests := []struct {
		name      string
		existKey  string
		passedKey string
	}{
		{
			name:      "key not exist passing nothing", // expected - generate new key
			existKey:  "",
			passedKey: "",
		},
		{
			name:      "key not exist passing key in env", // expected - set the passed key
			existKey:  "",
			passedKey: sk2,
		},
		{
			name:      "key exist passing key in env", // expected - override current key with the passed one
			existKey:  sk,
			passedKey: sk2,
		},
		{
			name:      "key exist passing nothing", // expected - do nothing
			existKey:  sk2,
			passedKey: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, err := kv.NewInMemory(logger, basedb.Options{})
			require.NoError(t, err)
			defer db.Close()

			p2pStorage := identityStore{
				db:     db,
				logger: logger,
			}

			if test.existKey != "" { // mock exist key
				privKey, err := gcrypto.HexToECDSA(test.existKey)
				require.NoError(t, err)
				require.NoError(t, p2pStorage.saveNetworkKey(privKey))
				sk, found, err := p2pStorage.GetNetworkKey()
				require.True(t, found)
				require.NoError(t, err)
				require.NotNil(t, sk)

				interfacePriv, err := commons.ECDSAPrivToInterface(privKey)
				require.NoError(t, err)
				b, err := interfacePriv.Raw()
				require.NoError(t, err)
				require.Equal(t, test.existKey, hex.EncodeToString(b))
			}

			_, err = p2pStorage.SetupNetworkKey(test.passedKey)
			require.NoError(t, err)
			privateKey, found, err := p2pStorage.GetNetworkKey()
			require.NoError(t, err)
			require.True(t, found)
			require.NoError(t, err)
			require.NotNil(t, privateKey)

			if test.existKey == "" && test.passedKey == "" { // new key generated
				return
			}
			if test.existKey != "" && test.passedKey == "" { // exist and not passed in env
				interfacePriv, err := commons.ECDSAPrivToInterface(privateKey)
				require.NoError(t, err)
				b, err := interfacePriv.Raw()
				require.NoError(t, err)
				require.Equal(t, test.existKey, hex.EncodeToString(b))
				return
			}
			// not exist && passed and exist && passed
			interfacePriv, err := commons.ECDSAPrivToInterface(privateKey)
			require.NoError(t, err)
			b, err := interfacePriv.Raw()
			require.NoError(t, err)
			require.Equal(t, test.passedKey, hex.EncodeToString(b))
		})
	}

	t.Run("NewIdentityStore", func(t *testing.T) {
		db, err := kv.NewInMemory(logger, basedb.Options{})
		require.NoError(t, err)
		defer db.Close()

		p2pStorage := NewIdentityStore(logger, db)

		require.NotNil(t, p2pStorage)
	})
}
