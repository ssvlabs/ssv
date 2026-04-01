package p2p

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	gcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/ssvsigner"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

var (
	sk                      = "ba03f90c6e2e6d67e4a4682621412ddbafeb6bffdc169df8f2bd31f193f001d4"
	sk2                     = "2340652c367bf8d17de1bc0454e6aa73e2eedd4a51686887d98d6b8813e5fb4a"
	networkKeyEncryptionKey = []byte("0123456789abcdef0123456789abcdef")
)

func newTestSSVSignerClient(t *testing.T) *ssvsigner.Client {
	mux := http.NewServeMux()
	mux.HandleFunc(ssvsigner.PathOperatorEncrypt, func(w http.ResponseWriter, r *http.Request) {
		payload, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		encrypted, err := keys.EncryptPayload(networkKeyEncryptionKey, payload)
		require.NoError(t, err)
		_, err = w.Write(encrypted)
		require.NoError(t, err)
	})
	mux.HandleFunc(ssvsigner.PathOperatorDecrypt, func(w http.ResponseWriter, r *http.Request) {
		payload, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		decrypted, err := keys.DecryptPayload(networkKeyEncryptionKey, payload)
		require.NoError(t, err)
		_, err = w.Write(decrypted)
		require.NoError(t, err)
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return ssvsigner.NewClient(server.URL, ssvsigner.WithLogger(zap.NewNop()))
}

type getOverrideDB struct {
	basedb.Database
	getFn func(prefix []byte, key []byte) (basedb.Obj, bool, error)
}

func (db getOverrideDB) Get(prefix []byte, key []byte) (basedb.Obj, bool, error) {
	if db.getFn != nil {
		return db.getFn(prefix, key)
	}
	return db.Database.Get(prefix, key)
}

func TestSetupPrivateKey(t *testing.T) {
	logger := log.TestLogger(t)
	ctx := context.Background()

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
				protectFn: func(_ context.Context, plaintext []byte) ([]byte, error) {
					return keys.EncryptPayload(networkKeyEncryptionKey, plaintext)
				},
				unprotectFn: func(_ context.Context, protectedValue []byte) ([]byte, error) {
					return keys.DecryptPayload(networkKeyEncryptionKey, protectedValue)
				},
			}

			if test.existKey != "" { // mock exist key
				privKey, err := gcrypto.HexToECDSA(test.existKey)
				require.NoError(t, err)
				require.NoError(t, p2pStorage.saveNetworkKey(ctx, privKey))
				sk, found, err := p2pStorage.GetNetworkKey(ctx)
				require.True(t, found)
				require.NoError(t, err)
				require.NotNil(t, sk)

				interfacePriv, err := commons.ECDSAPrivToInterface(privKey)
				require.NoError(t, err)
				b, err := interfacePriv.Raw()
				require.NoError(t, err)
				require.Equal(t, test.existKey, hex.EncodeToString(b))
			}

			_, err = p2pStorage.SetupNetworkKey(ctx, test.passedKey)
			require.NoError(t, err)
			privateKey, found, err := p2pStorage.GetNetworkKey(ctx)
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

	t.Run("migrates legacy plaintext key to encrypted storage", func(t *testing.T) {
		db, err := kv.NewInMemory(logger, basedb.Options{})
		require.NoError(t, err)
		defer db.Close()

		privKey, err := gcrypto.HexToECDSA(sk)
		require.NoError(t, err)
		require.NoError(t, db.Set(prefix, netKeyPrefix, gcrypto.FromECDSA(privKey)))

		p2pStorage := identityStore{
			db:     db,
			logger: logger,
			protectFn: func(_ context.Context, plaintext []byte) ([]byte, error) {
				return keys.EncryptPayload(networkKeyEncryptionKey, plaintext)
			},
			unprotectFn: func(_ context.Context, protectedValue []byte) ([]byte, error) {
				return keys.DecryptPayload(networkKeyEncryptionKey, protectedValue)
			},
		}

		storedBefore, found, err := db.Get(prefix, netKeyPrefix)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, gcrypto.FromECDSA(privKey), storedBefore.Value)

		loadedKey, err := p2pStorage.SetupNetworkKey(ctx, "")
		require.NoError(t, err)
		require.NotNil(t, loadedKey)

		storedAfter, found, err := db.Get(prefix, netKeyPrefix)
		require.NoError(t, err)
		require.True(t, found)
		require.True(t, bytes.HasPrefix(storedAfter.Value, encryptedPrefix))
		require.NotEqual(t, gcrypto.FromECDSA(privKey), storedAfter.Value)
	})

	t.Run("persists configured key in legacy plaintext when encryption secret is missing", func(t *testing.T) {
		db, err := kv.NewInMemory(logger, basedb.Options{})
		require.NoError(t, err)
		defer db.Close()

		p2pStorage := identityStore{
			db:     db,
			logger: logger,
		}

		privateKey, err := p2pStorage.SetupNetworkKey(ctx, sk)
		require.NoError(t, err)
		require.NotNil(t, privateKey)

		storedAfter, found, err := db.Get(prefix, netKeyPrefix)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, gcrypto.FromECDSA(privateKey), storedAfter.Value)
	})

	t.Run("uses legacy plaintext key from storage when encryption secret is missing", func(t *testing.T) {
		db, err := kv.NewInMemory(logger, basedb.Options{})
		require.NoError(t, err)
		defer db.Close()

		privKey, err := gcrypto.HexToECDSA(sk)
		require.NoError(t, err)
		legacyEncoded := gcrypto.FromECDSA(privKey)
		require.NoError(t, db.Set(prefix, netKeyPrefix, legacyEncoded))

		p2pStorage := identityStore{
			db:     db,
			logger: logger,
		}

		privateKey, err := p2pStorage.SetupNetworkKey(ctx, "")
		require.NoError(t, err)
		require.NotNil(t, privateKey)

		storedAfter, found, err := db.Get(prefix, netKeyPrefix)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, legacyEncoded, storedAfter.Value)
	})

	t.Run("generates and persists a legacy plaintext key when encryption secret is missing", func(t *testing.T) {
		db, err := kv.NewInMemory(logger, basedb.Options{})
		require.NoError(t, err)
		defer db.Close()

		p2pStorage := identityStore{
			db:     db,
			logger: logger,
		}

		privateKey, err := p2pStorage.SetupNetworkKey(ctx, "")
		require.NoError(t, err)
		require.NotNil(t, privateKey)

		storedAfter, found, err := db.Get(prefix, netKeyPrefix)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, gcrypto.FromECDSA(privateKey), storedAfter.Value)
	})

	t.Run("returns DB errors from GetNetworkKey even when key is not found", func(t *testing.T) {
		db, err := kv.NewInMemory(logger, basedb.Options{})
		require.NoError(t, err)
		defer db.Close()

		expectedErr := errors.New("db read failed")
		p2pStorage := identityStore{
			db: getOverrideDB{
				Database: db,
				getFn: func(prefix []byte, key []byte) (basedb.Obj, bool, error) {
					return basedb.Obj{}, false, expectedErr
				},
			},
			logger: logger,
		}

		_, _, err = p2pStorage.GetNetworkKey(ctx)
		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("returns decode-specific error when stored key cannot be decrypted", func(t *testing.T) {
		db, err := kv.NewInMemory(logger, basedb.Options{})
		require.NoError(t, err)
		defer db.Close()

		privateKey, err := gcrypto.HexToECDSA(sk)
		require.NoError(t, err)

		p2pStorage := identityStore{
			db:     db,
			logger: logger,
			protectFn: func(_ context.Context, plaintext []byte) ([]byte, error) {
				return keys.EncryptPayload(networkKeyEncryptionKey, plaintext)
			},
			unprotectFn: func(_ context.Context, protectedValue []byte) ([]byte, error) {
				return bytes.Repeat([]byte{0xff}, 32), nil
			},
		}

		require.NoError(t, p2pStorage.saveNetworkKey(ctx, privateKey))

		_, err = p2pStorage.SetupNetworkKey(ctx, "")
		require.ErrorContains(t, err, "decode network key")
	})

	t.Run("NewIdentityStore", func(t *testing.T) {
		db, err := kv.NewInMemory(logger, basedb.Options{})
		require.NoError(t, err)
		defer db.Close()

		p2pStorage := NewIdentityStore(
			logger,
			db,
			func(_ context.Context, plaintext []byte) ([]byte, error) {
				return keys.EncryptPayload(networkKeyEncryptionKey, plaintext)
			},
			func(_ context.Context, protectedValue []byte) ([]byte, error) {
				return keys.DecryptPayload(networkKeyEncryptionKey, protectedValue)
			},
		)

		require.NotNil(t, p2pStorage)
	})
}

func TestEKMEncryptionKey(t *testing.T) {
	operatorPrivKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	encryptionKey, err := operatorPrivKey.EKMEncryptionKey()
	require.NoError(t, err)
	require.Len(t, encryptionKey, 32)

	encryptionKey2, err := operatorPrivKey.EKMEncryptionKey()
	require.NoError(t, err)
	require.Equal(t, encryptionKey, encryptionKey2)
}

func TestEncryptedNetworkKey(t *testing.T) {
	logger := log.TestLogger(t)
	ctx := context.Background()

	t.Run("stores and loads encrypted network key", func(t *testing.T) {
		db, err := kv.NewInMemory(logger, basedb.Options{})
		require.NoError(t, err)
		defer db.Close()

		p2pStorage := identityStore{
			db:     db,
			logger: logger,
			protectFn: func(_ context.Context, plaintext []byte) ([]byte, error) {
				return keys.EncryptPayload(networkKeyEncryptionKey, plaintext)
			},
			unprotectFn: func(_ context.Context, protectedValue []byte) ([]byte, error) {
				return keys.DecryptPayload(networkKeyEncryptionKey, protectedValue)
			},
		}

		privateKey, err := p2pStorage.SetupNetworkKey(ctx, sk)
		require.NoError(t, err)
		require.NotNil(t, privateKey)

		storedObj, found, err := db.Get(prefix, netKeyPrefix)
		require.NoError(t, err)
		require.True(t, found)
		require.True(t, bytes.HasPrefix(storedObj.Value, encryptedPrefix))

		loadedKey, found, err := p2pStorage.GetNetworkKey(ctx)
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, loadedKey)

		interfacePriv, err := commons.ECDSAPrivToInterface(loadedKey)
		require.NoError(t, err)
		raw, err := interfacePriv.Raw()
		require.NoError(t, err)
		require.Equal(t, sk, hex.EncodeToString(raw))
	})

	t.Run("detects encrypted network key format", func(t *testing.T) {
		db, err := kv.NewInMemory(logger, basedb.Options{})
		require.NoError(t, err)
		defer db.Close()

		p2pStorage := identityStore{
			db:     db,
			logger: logger,
			protectFn: func(_ context.Context, plaintext []byte) ([]byte, error) {
				return keys.EncryptPayload(networkKeyEncryptionKey, plaintext)
			},
			unprotectFn: func(_ context.Context, protectedValue []byte) ([]byte, error) {
				return keys.DecryptPayload(networkKeyEncryptionKey, protectedValue)
			},
		}

		_, err = p2pStorage.SetupNetworkKey(ctx, sk)
		require.NoError(t, err)

		hasEncryptedKey, err := HasEncryptedNetworkKey(db)
		require.NoError(t, err)
		require.True(t, hasEncryptedKey)
	})

	t.Run("errors clearly when encrypted key cannot be decrypted", func(t *testing.T) {
		db, err := kv.NewInMemory(logger, basedb.Options{})
		require.NoError(t, err)
		defer db.Close()

		p2pStorage := identityStore{
			db:     db,
			logger: logger,
			protectFn: func(_ context.Context, plaintext []byte) ([]byte, error) {
				return keys.EncryptPayload(networkKeyEncryptionKey, plaintext)
			},
			unprotectFn: func(_ context.Context, protectedValue []byte) ([]byte, error) {
				return keys.DecryptPayload(networkKeyEncryptionKey, protectedValue)
			},
		}

		_, err = p2pStorage.SetupNetworkKey(ctx, sk)
		require.NoError(t, err)

		storeWithoutProtector := identityStore{
			db:     db,
			logger: logger,
		}

		_, _, err = storeWithoutProtector.GetNetworkKey(ctx)
		require.ErrorContains(t, err, "network key is encrypted but no compatible network key protector is configured")
	})

	t.Run("stores and loads remotely protected network key using encrypted format", func(t *testing.T) {
		db, err := kv.NewInMemory(logger, basedb.Options{})
		require.NoError(t, err)
		defer db.Close()

		signerClient := newTestSSVSignerClient(t)
		p2pStorage := identityStore{
			db:          db,
			logger:      logger,
			protectFn:   signerClient.OperatorEncrypt,
			unprotectFn: signerClient.OperatorDecrypt,
		}

		privateKey, err := p2pStorage.SetupNetworkKey(ctx, sk)
		require.NoError(t, err)
		require.NotNil(t, privateKey)

		storedObj, found, err := db.Get(prefix, netKeyPrefix)
		require.NoError(t, err)
		require.True(t, found)
		require.True(t, bytes.HasPrefix(storedObj.Value, encryptedPrefix))

		loadedKey, found, err := p2pStorage.GetNetworkKey(ctx)
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, loadedKey)
	})

	t.Run("detects remotely protected network key as encrypted format", func(t *testing.T) {
		db, err := kv.NewInMemory(logger, basedb.Options{})
		require.NoError(t, err)
		defer db.Close()

		signerClient := newTestSSVSignerClient(t)
		p2pStorage := identityStore{
			db:          db,
			logger:      logger,
			protectFn:   signerClient.OperatorEncrypt,
			unprotectFn: signerClient.OperatorDecrypt,
		}

		_, err = p2pStorage.SetupNetworkKey(ctx, sk)
		require.NoError(t, err)

		hasEncryptedKey, err := HasEncryptedNetworkKey(db)
		require.NoError(t, err)
		require.True(t, hasEncryptedKey)
	})

	t.Run("errors clearly when remotely protected key cannot be unprotected", func(t *testing.T) {
		db, err := kv.NewInMemory(logger, basedb.Options{})
		require.NoError(t, err)
		defer db.Close()

		signerClient := newTestSSVSignerClient(t)
		p2pStorage := identityStore{
			db:          db,
			logger:      logger,
			protectFn:   signerClient.OperatorEncrypt,
			unprotectFn: signerClient.OperatorDecrypt,
		}

		_, err = p2pStorage.SetupNetworkKey(ctx, sk)
		require.NoError(t, err)

		storeWithoutProtector := identityStore{
			db:     db,
			logger: logger,
		}

		_, _, err = storeWithoutProtector.GetNetworkKey(ctx)
		require.ErrorContains(t, err, "network key is encrypted but no compatible network key protector is configured")
	})
}
