package operator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	dgbadger "github.com/dgraph-io/badger/v4"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"golang.org/x/crypto/hkdf"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/ekmadapter"
	"github.com/ssvlabs/ssv/networkconfig"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/ssvsigner"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/ssvsigner/keys/rsaencryption"
	"github.com/ssvlabs/ssv/ssvsigner/keys/rsatesting"
	"github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// newMissingKeysClient returns an ssv-signer client whose validators endpoint —
// the one MissingKeys queries — is served by handler.
func newMissingKeysClient(t *testing.T, handler http.HandlerFunc) *ssvsigner.Client {
	return newTestSSVSignerClient(t, func(mux *http.ServeMux) {
		mux.HandleFunc(ssvsigner.PathValidators, handler)
	})
}

func Test_fetchMissingKeysWithRetry(t *testing.T) {
	localKeys := []phase0.BLSPubKey{{1, 2, 3}}

	t.Run("succeeds after transient failures", func(t *testing.T) {
		var hits int
		client := newMissingKeysClient(t, func(w http.ResponseWriter, r *http.Request) {
			hits++
			if hits <= 2 {
				http.Error(w, "web3signer starting up", http.StatusInternalServerError)
				return
			}
			require.NoError(t, json.NewEncoder(w).Encode(localKeys))
		})

		missingKeys, err := fetchMissingKeysWithRetry(
			context.Background(), zap.NewNop(), client, localKeys,
			retryBackoff{window: time.Minute, delay: time.Millisecond, maxDelay: 16 * time.Millisecond})
		require.NoError(t, err)
		require.Empty(t, missingKeys)
		require.Equal(t, 3, hits)
	})

	t.Run("gives up when the window elapses", func(t *testing.T) {
		var hits int
		client := newMissingKeysClient(t, func(w http.ResponseWriter, r *http.Request) {
			hits++
			http.Error(w, "permanent failure", http.StatusInternalServerError)
		})

		_, err := fetchMissingKeysWithRetry(
			context.Background(), zap.NewNop(), client, localKeys,
			retryBackoff{window: 200 * time.Millisecond, delay: 20 * time.Millisecond, maxDelay: time.Second})
		require.ErrorContains(t, err, "unexpected status: 500")
		require.Greater(t, hits, 1, "expected at least one retry within the window")
	})

	// Cancellation during a request: the helper bails at the ctx.Err() check,
	// before reaching any backoff wait.
	t.Run("stops retrying when ctx is canceled during a request", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		client := newMissingKeysClient(t, func(w http.ResponseWriter, r *http.Request) {
			cancel()
			http.Error(w, "failure", http.StatusInternalServerError)
		})

		start := time.Now()
		_, err := fetchMissingKeysWithRetry(
			ctx, zap.NewNop(), client, localKeys,
			retryBackoff{window: time.Hour, delay: time.Hour, maxDelay: time.Hour})
		require.Error(t, err)
		require.Less(t, time.Since(start), time.Minute, "expected to give up without waiting out the backoff")
	})

	// Cancellation during the backoff wait (the select), not a request — the path
	// that keeps shutdown prompt when a signal lands mid-backoff.
	t.Run("aborts the backoff wait when ctx is canceled between attempts", func(t *testing.T) {
		client := newMissingKeysClient(t, func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "failure", http.StatusInternalServerError)
		})

		ctx, cancel := context.WithCancel(context.Background())
		// First attempt fails fast; cancel while the retry is parked on its long
		// backoff, so cancellation interrupts the wait itself.
		go func() {
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()

		done := make(chan error, 1)
		go func() {
			_, err := fetchMissingKeysWithRetry(
				ctx, zap.NewNop(), client, localKeys,
				retryBackoff{window: time.Hour, delay: 10 * time.Second, maxDelay: time.Minute})
			done <- err
		}()

		select {
		case err := <-done:
			require.Error(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("expected the backoff wait to be interrupted by ctx cancellation")
		}
	})

	// Drive several retries against a small injected cap and assert the logged
	// retry_in delays double from the initial value and then plateau at the cap.
	t.Run("caps the backoff delay at maxDelay", func(t *testing.T) {
		const stopAfter = 6 // enough retries to reach the cap and stay there

		core, logs := observer.New(zapcore.DebugLevel)
		ctx, cancel := context.WithCancel(context.Background())

		var hits int
		client := newMissingKeysClient(t, func(w http.ResponseWriter, r *http.Request) {
			hits++
			if hits >= stopAfter {
				cancel() // stop retrying once enough backoffs are logged
			}
			http.Error(w, "web3signer starting up", http.StatusInternalServerError)
		})

		initialDelay, maxDelay := time.Millisecond, 4*time.Millisecond
		_, err := fetchMissingKeysWithRetry(
			ctx, zap.New(core), client, localKeys,
			retryBackoff{window: time.Hour, delay: initialDelay, maxDelay: maxDelay})
		require.Error(t, err)

		entries := logs.All()
		delays := make([]time.Duration, 0, len(entries))
		for _, entry := range entries {
			d, ok := entry.ContextMap()["retry_in"].(time.Duration)
			require.True(t, ok, "retry log is missing a retry_in duration")
			delays = append(delays, d)
		}
		require.GreaterOrEqual(t, len(delays), 4, "need enough retries to observe the plateau")

		want := initialDelay
		for i, got := range delays {
			require.Equal(t, want, got, "unexpected backoff at retry %d", i)
			want = min(want*2, maxDelay)
		}
		require.Equal(t, maxDelay, delays[len(delays)-1], "backoff should plateau at the cap")
	})
}

func Test_operatorKeyCheckValue(t *testing.T) {
	operatorPrivKey, err := keys.PrivateKeyFromBytes([]byte(rsatesting.PrivKeyPEM))
	require.NoError(t, err)

	// Pinned: a change would make every upgraded node refuse its database.
	require.Equal(t, "a19ffd874a4d89c3450f2f30006e938552cd608606f480b555fdfbd07a3a9b7d", hex.EncodeToString(checkValueOf(t, operatorPrivKey)))
}

func Test_ensureOperatorPrivateKey(t *testing.T) {
	openNodeStorage := func(t *testing.T, dbPath string) (*badger.DB, operatorstorage.Storage) {
		db, err := badger.New(zap.NewNop(), basedb.Options{Path: dbPath})
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })

		nodeStorage, err := operatorstorage.NewNodeStorage(networkconfig.TestNetwork.Beacon, zap.NewNop(), db)
		require.NoError(t, err)
		return db, nodeStorage
	}

	t.Run("saves the check value on first run and accepts it afterwards", func(t *testing.T) {
		_, nodeStorage := openNodeStorage(t, t.TempDir())
		operatorPrivKey := newOperatorPrivateKey(t)
		checkValue := checkValueOf(t, operatorPrivKey)

		require.NoError(t, ensureOperatorPrivateKey(zap.NewNop(), nodeStorage, operatorPrivKey, operatorPrivKey.Base64()))
		requireKeyChecks(t, nodeStorage, checkValue, nil)

		require.NoError(t, ensureOperatorPrivateKey(zap.NewNop(), nodeStorage, operatorPrivKey, operatorPrivKey.Base64()))
		requireKeyChecks(t, nodeStorage, checkValue, nil)
	})

	t.Run("rejects a different key", func(t *testing.T) {
		_, nodeStorage := openNodeStorage(t, t.TempDir())
		operatorPrivKey := newOperatorPrivateKey(t)
		checkValue := checkValueOf(t, operatorPrivKey)
		require.NoError(t, ensureOperatorPrivateKey(zap.NewNop(), nodeStorage, operatorPrivKey, operatorPrivKey.Base64()))

		otherPrivKey := newOperatorPrivateKey(t)
		err := ensureOperatorPrivateKey(zap.NewNop(), nodeStorage, otherPrivKey, otherPrivKey.Base64())
		require.ErrorIs(t, err, errOperatorKeyMismatch)
		requireKeyChecks(t, nodeStorage, checkValue, nil)
	})

	// As an older version starting on the database again, or an interrupted drop, leaves it.
	t.Run("drops a hash stored next to the check value", func(t *testing.T) {
		db, nodeStorage := openNodeStorage(t, t.TempDir())
		operatorPrivKey := newOperatorPrivateKey(t)
		checkValue := checkValueOf(t, operatorPrivKey)
		require.NoError(t, nodeStorage.SaveOperatorKeyCheckValue(checkValue))
		saveLegacyPrivateKeyHash(t, db, operatorPrivKey.StorageHash())

		otherPrivKey := newOperatorPrivateKey(t)
		err := ensureOperatorPrivateKey(zap.NewNop(), nodeStorage, otherPrivKey, otherPrivKey.Base64())
		require.ErrorIs(t, err, errOperatorKeyMismatch)
		requireKeyChecks(t, nodeStorage, checkValue, operatorPrivKey.StorageHash())

		require.NoError(t, ensureOperatorPrivateKey(zap.NewNop(), nodeStorage, operatorPrivKey, operatorPrivKey.Base64()))
		requireKeyChecks(t, nodeStorage, checkValue, nil)
	})

	// An interrupted start must leave one of the two, for the next start to check the key against.
	t.Run("keeps the hash until the check value is saved", func(t *testing.T) {
		db, nodeStorage := openNodeStorage(t, t.TempDir())
		operatorPrivKey := newOperatorPrivateKey(t)
		saveLegacyPrivateKeyHash(t, db, operatorPrivKey.StorageHash())

		err := ensureOperatorPrivateKey(zap.NewNop(), failSaveCheckValueStorage{nodeStorage}, operatorPrivKey, operatorPrivKey.Base64())
		require.ErrorContains(t, err, "could not save operator key check value")
		requireKeyChecks(t, nodeStorage, nil, operatorPrivKey.StorageHash())
	})

	// Hashes older versions store, with the configured key text they are checked against. That text
	// lacks the PEM's trailing newline, so its hash differs from StorageHash: each hash matches only its
	// own comparison.
	keyText := func(operatorPrivKey keys.OperatorPrivateKey) []byte {
		return bytes.TrimSuffix(operatorPrivKey.Bytes(), []byte("\n"))
	}
	legacyHashes := map[string]func(keys.OperatorPrivateKey) (hash []byte, configuredKey string){
		"StorageHash": func(operatorPrivKey keys.OperatorPrivateKey) ([]byte, string) {
			return operatorPrivKey.StorageHash(), base64.StdEncoding.EncodeToString(keyText(operatorPrivKey))
		},
		"hash of the configured key text": func(operatorPrivKey keys.OperatorPrivateKey) ([]byte, string) {
			return rsaencryption.HashKeyBytes(keyText(operatorPrivKey)), base64.StdEncoding.EncodeToString(keyText(operatorPrivKey))
		},
	}
	for name, legacyHash := range legacyHashes {
		t.Run("drops a matching "+name, func(t *testing.T) {
			dbPath := t.TempDir()
			db, _ := openNodeStorage(t, dbPath)
			operatorPrivKey := newOperatorPrivateKey(t)
			hash, configuredKey := legacyHash(operatorPrivKey)
			saveLegacyPrivateKeyHash(t, db, hash)
			saveLegacyPrivateKeyHash(t, db, hash) // a second version, as an overwrite leaves
			require.NoError(t, db.Close())

			// Reopen with the hash below level 0, as on a node that ran an older version for a while:
			// dropping any prefix compacts all of level 0 down.
			db, nodeStorage := openNodeStorage(t, dbPath)
			require.NoError(t, db.Set([]byte("filler/"), []byte("key"), []byte("value")))
			require.NoError(t, db.DropPrefix([]byte("filler/")))
			for _, table := range db.Badger().Tables() {
				require.NotZero(t, table.Level)
			}
			require.True(t, isStored(t, db, []byte(hex.EncodeToString(hash))))
			logCore, logs := observer.New(zapcore.InfoLevel)

			require.NoError(t, ensureOperatorPrivateKey(zap.New(logCore), nodeStorage, operatorPrivKey, configuredKey))
			requireKeyChecks(t, nodeStorage, checkValueOf(t, operatorPrivKey), nil)
			require.False(t, isStored(t, db, []byte(hex.EncodeToString(hash))), "a version of the hash is left")
			require.Equal(t, 1, logs.FilterMessage("dropped the stored private key hash").Len())
		})

		t.Run("rejects a "+name+" of a different key", func(t *testing.T) {
			db, nodeStorage := openNodeStorage(t, t.TempDir())
			hash, _ := legacyHash(newOperatorPrivateKey(t))
			saveLegacyPrivateKeyHash(t, db, hash)

			operatorPrivKey := newOperatorPrivateKey(t)
			err := ensureOperatorPrivateKey(zap.NewNop(), nodeStorage, operatorPrivKey, operatorPrivKey.Base64())
			require.ErrorIs(t, err, errOperatorKeyMismatch)
			requireKeyChecks(t, nodeStorage, nil, hash)
		})
	}
}

// TestShareKeysNotRecoverableFromDatabase checks that the database alone doesn't decrypt the share
// keys: that takes the operator private key.
func TestShareKeysNotRecoverableFromDatabase(t *testing.T) {
	require.NoError(t, bls.Init(bls.BLS12_381))
	require.NoError(t, bls.SetETHmode(bls.EthModeDraft07))

	logger := zap.NewNop()
	beaconConfig := networkconfig.TestNetwork.Beacon

	// startNode opens the database at dbPath and applies the migrations, as a node does on start.
	startNode := func(t *testing.T, dbPath string, operatorPrivKey keys.OperatorPrivateKey) (*badger.DB, operatorstorage.Storage) {
		db, err := badger.New(logger, basedb.Options{Path: dbPath})
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })

		cfg := &config{DBOptions: basedb.Options{Ctx: t.Context(), Path: dbPath}}
		require.NoError(t, applyMigrations(logger, cfg, beaconConfig, operatorPrivKey, db, dbPath))

		nodeStorage, err := operatorstorage.NewNodeStorage(beaconConfig, logger, db)
		require.NoError(t, err)
		return db, nodeStorage
	}
	newKeyManager := func(t *testing.T, db *badger.DB, operatorPrivKey keys.OperatorPrivateKey) *ekm.LocalKeyManager {
		km, err := ekm.NewLocalKeyManager(logger, ekmadapter.NewDatabaseAdapter(db), beaconConfig, operatorPrivKey)
		require.NoError(t, err)
		return km
	}
	// v250Database leaves a database as v2.5.0 does, with StorageHash stored next to the share key,
	// all in on-disk tables.
	v250Database := func(t *testing.T, operatorPrivKey keys.OperatorPrivateKey) (dbPath string, sharePubKey phase0.BLSPubKey) {
		dbPath = t.TempDir()
		db, _ := startNode(t, dbPath, operatorPrivKey)
		saveLegacyPrivateKeyHash(t, db, operatorPrivKey.StorageHash())
		sharePubKey = addShare(t, newKeyManager(t, db, operatorPrivKey), operatorPrivKey.Public())
		require.True(t, isStored(t, db, []byte(hex.EncodeToString(operatorPrivKey.StorageHash()))), "the v2.5.0 database should hold the hash, or this test checks nothing")
		require.NoError(t, db.Close())
		return dbPath, sharePubKey
	}

	t.Run("new node", func(t *testing.T) {
		operatorPrivKey := newOperatorPrivateKey(t)
		db, nodeStorage := startNode(t, t.TempDir(), operatorPrivKey)
		require.NoError(t, ensureOperatorPrivateKey(logger, nodeStorage, operatorPrivKey, operatorPrivKey.Base64()))
		km := newKeyManager(t, db, operatorPrivKey)
		sharePubKey := addShare(t, km, operatorPrivKey.Public())

		requireSigns(t, km, sharePubKey)
		require.False(t, shareKeysRecoverable(t, db), "the database alone decrypts the share key")
	})

	t.Run("node upgraded from v2.5.0", func(t *testing.T) {
		operatorPrivKey := newOperatorPrivateKey(t)
		dbPath, sharePubKey := v250Database(t, operatorPrivKey)

		db, nodeStorage := startNode(t, dbPath, operatorPrivKey)
		require.NoError(t, ensureOperatorPrivateKey(logger, nodeStorage, operatorPrivKey, operatorPrivKey.Base64()))

		requireSigns(t, newKeyManager(t, db, operatorPrivKey), sharePubKey)
		require.False(t, shareKeysRecoverable(t, db), "the database alone decrypts the share key")
	})

	t.Run("node rolled back to v2.5.0 and upgraded again", func(t *testing.T) {
		operatorPrivKey := newOperatorPrivateKey(t)
		dbPath, sharePubKey := v250Database(t, operatorPrivKey)
		db, nodeStorage := startNode(t, dbPath, operatorPrivKey)
		require.NoError(t, ensureOperatorPrivateKey(logger, nodeStorage, operatorPrivKey, operatorPrivKey.Base64()))

		// v2.5.0 stores the hash again, as on a database without one, and still decrypts the share key.
		saveLegacyPrivateKeyHash(t, db, operatorPrivKey.StorageHash())
		requireSigns(t, newKeyManager(t, db, operatorPrivKey), sharePubKey)

		require.NoError(t, ensureOperatorPrivateKey(logger, nodeStorage, operatorPrivKey, operatorPrivKey.Base64()))
		require.False(t, shareKeysRecoverable(t, db), "the database alone decrypts the share key")
	})
}

func newOperatorPrivateKey(t *testing.T) keys.OperatorPrivateKey {
	t.Helper()

	operatorPrivKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)
	return operatorPrivKey
}

func checkValueOf(t *testing.T, operatorPrivKey keys.OperatorPrivateKey) []byte {
	t.Helper()

	operatorPubKeyBase64, err := operatorPrivKey.Public().Base64()
	require.NoError(t, err)
	return operatorKeyCheckValue(operatorPubKeyBase64)
}

// requireKeyChecks asserts the stored check value and private key hash, nil meaning none.
func requireKeyChecks(t *testing.T, nodeStorage operatorstorage.Storage, checkValue, privKeyHash []byte) {
	t.Helper()

	stored, found, err := nodeStorage.GetOperatorKeyCheckValue()
	require.NoError(t, err)
	require.Equal(t, checkValue != nil, found)
	require.Equal(t, checkValue, stored)

	stored, found, err = nodeStorage.GetPrivateKeyHash()
	require.NoError(t, err)
	require.Equal(t, privKeyHash != nil, found)
	require.Equal(t, privKeyHash, stored)
}

// saveLegacyPrivateKeyHash stores a private key hash the way older versions do.
func saveLegacyPrivateKeyHash(t *testing.T, db basedb.Database, hash []byte) {
	t.Helper()

	require.NoError(t, db.Set(operatorstorage.OperatorStoragePrefix, []byte("hashed-private-key"), []byte(hex.EncodeToString(hash))))
}

// failSaveCheckValueStorage fails to save the operator key check value.
type failSaveCheckValueStorage struct {
	operatorstorage.Storage
}

func (failSaveCheckValueStorage) SaveOperatorKeyCheckValue([]byte) error {
	return errors.New("save failed")
}

func addShare(t *testing.T, km *ekm.LocalKeyManager, operatorPubKey keys.OperatorPublicKey) phase0.BLSPubKey {
	t.Helper()

	var shareSecretKey bls.SecretKey
	shareSecretKey.SetByCSPRNG()
	encryptedShare, err := operatorPubKey.Encrypt([]byte(shareSecretKey.SerializeToHexStr()))
	require.NoError(t, err)

	sharePubKey := phase0.BLSPubKey(shareSecretKey.GetPublicKey().Serialize())
	require.NoError(t, km.AddShare(t.Context(), encryptedShare, sharePubKey))
	return sharePubKey
}

func requireSigns(t *testing.T, km *ekm.LocalKeyManager, sharePubKey phase0.BLSPubKey) {
	t.Helper()

	var domain phase0.Domain
	copy(domain[:], spectypes.DomainRandao[:])
	sig, root, err := km.SignBeaconObject(t.Context(), spectypes.SSZUint64(1), domain, sharePubKey, 32, spectypes.DomainRandao)
	require.NoError(t, err)

	var pubKey bls.PublicKey
	require.NoError(t, pubKey.Deserialize(sharePubKey[:]))
	var signature bls.Sign
	require.NoError(t, signature.Deserialize(sig))
	require.True(t, signature.VerifyByte(&pubKey, root[:]))
}

// storedValues returns every stored version of every value in db, by key.
func storedValues(t *testing.T, db *badger.DB) map[string][][]byte {
	t.Helper()

	values := make(map[string][][]byte)
	err := db.Badger().View(func(txn *dgbadger.Txn) error {
		opts := dgbadger.DefaultIteratorOptions
		opts.AllVersions = true
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			if item.IsDeletedOrExpired() {
				continue
			}
			value, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}
			values[string(item.Key())] = append(values[string(item.Key())], value)
		}
		return nil
	})
	require.NoError(t, err)
	return values
}

// isStored reports whether any version of any value in db equals value.
func isStored(t *testing.T, db *badger.DB, value []byte) bool {
	t.Helper()

	for _, versions := range storedValues(t, db) {
		for _, stored := range versions {
			if bytes.Equal(stored, value) {
				return true
			}
		}
	}
	return false
}

// shareKeysRecoverable reports whether db alone decrypts a share key. It tries every stored value
// (all versions, raw and hex-decoded) as the key, and as HKDF input with the labels of the operator
// key's encryption keys, against every version of every share key.
func shareKeysRecoverable(t *testing.T, db *badger.DB) bool {
	t.Helper()

	var encryptedShares, candidates [][]byte
	for key, versions := range storedValues(t, db) {
		for _, value := range versions {
			if strings.Contains(key, "signer_data-accounts-") {
				encryptedShares = append(encryptedShares, value)
			}
			candidates = append(candidates, value)
			if decoded, err := hex.DecodeString(string(value)); err == nil {
				candidates = append(candidates, decoded)
			}
		}
	}
	require.NotEmpty(t, encryptedShares)

	// HKDF info labels: EKMEncryptionKey's (none) and DataProtectionKey's.
	infos := []string{"", "ssv/operator-data-protection-key/v1"}
	for _, candidate := range candidates {
		encryptionKeys := make([][]byte, 0, 1+len(infos))
		encryptionKeys = append(encryptionKeys, candidate)
		for _, info := range infos {
			derived := make([]byte, 32)
			_, err := io.ReadFull(hkdf.New(sha256.New, candidate, nil, []byte(info)), derived)
			require.NoError(t, err)
			encryptionKeys = append(encryptionKeys, derived)
		}

		for _, encryptionKey := range encryptionKeys {
			for _, encryptedShare := range encryptedShares {
				if _, err := keys.DecryptPayload(encryptionKey, encryptedShare); err == nil {
					return true
				}
			}
		}
	}
	return false
}
