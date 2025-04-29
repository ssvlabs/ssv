package keystore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/ssvsigner/keys"
)

const (
	testPassword     = "password"
	testPubKeyBase64 = "base64EncodedPublicKey"
)

func TestMain(m *testing.M) {
	if err := bls.Init(bls.BLS12_381); err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize BLS: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func TestDecryptKeystore(t *testing.T) {
	t.Parallel()

	t.Run("with invalid data", func(t *testing.T) {
		t.Parallel()

		encryptedJSONData := []byte(`{"version":4,"pubkey":"` + testPubKeyBase64 + `","crypto":{"kdf":"scrypt","checksum":{"function":"sha256","params":{"dklen":32,"salt":"base64EncodedSalt"},"message":"base64EncodedMessage"},"cipher":{"function":"aes-128-ctr","params":{"iv":"base64EncodedIV"},"message":"base64EncodedEncryptedMessage"},"kdfparams":{"n":262144,"r":8,"p":1,"salt":"base64EncodedSalt"}}}`)
		_, err := DecryptKeystore(encryptedJSONData, testPassword)
		require.Error(t, err)
	})

	t.Run("with empty password", func(t *testing.T) {
		t.Parallel()

		password := ""
		encryptedJSONData := []byte(`{"valid":"data"}`)
		_, err := DecryptKeystore(encryptedJSONData, password)
		require.Error(t, err)
		require.Contains(t, err.Error(), "password required")
	})

	t.Run("with invalid checksum", func(t *testing.T) {
		t.Parallel()

		invalidChecksumData := []byte(`{"checksum":{"function":"SHA256","message":"db27fe860c96f269f7838525ba8dce0886e0b7753caccc14162195bcdacbf49e","params":{}},"cipher":{"function":"xor","message":"e18afad793ec8dc3263169c07add77515d9f301464a05508d7ecb42ced24ed3a","params":{}},"kdf":{"function":"scrypt","message":"","params":{"dklen":32,"n":262144,"p":8,"r":1,"salt":"ab0c7876052600dd703518d6fc3fe8984592145b591fc8fb5c6d43190334ba19"}}}`)
		_, err := DecryptKeystore(invalidChecksumData, testPassword)
		require.Error(t, err)
		require.Contains(t, err.Error(), "decrypt private key")
	})
}

func TestEncryptKeystore(t *testing.T) {
	t.Parallel()

	t.Run("with valid data", func(t *testing.T) {
		t.Parallel()

		privkey := []byte("privateKey")

		data, err := EncryptKeystore(privkey, testPubKeyBase64, testPassword)
		require.NoError(t, err)

		var jsonData map[string]interface{}
		err = json.Unmarshal(data, &jsonData)
		require.NoError(t, err)
		require.Equal(t, testPubKeyBase64, jsonData["pubkey"])

		decrtypted, err := DecryptKeystore(data, testPassword)
		require.NoError(t, err)
		require.Equal(t, privkey, decrtypted)
	})

	t.Run("with empty password", func(t *testing.T) {
		t.Parallel()

		password := ""
		privkey := []byte("privateKey")

		_, err := EncryptKeystore(privkey, testPubKeyBase64, password)
		require.Error(t, err)
		require.Contains(t, err.Error(), "password required")
	})

	t.Run("with nil private key", func(t *testing.T) {
		t.Parallel()

		var privkey []byte = nil

		_, err := EncryptKeystore(privkey, testPubKeyBase64, testPassword)
		require.Error(t, err)
		require.Contains(t, err.Error(), "encrypt private key")
	})
}

func TestLoadOperatorKeystore(t *testing.T) {
	t.Parallel()

	t.Run("fails when encryptedPrivateKeyFile does not exist", func(t *testing.T) {
		t.Parallel()

		nonExistentFile := filepath.Join(os.TempDir(), "nonexistent.pem")
		passwordFile := filepath.Join(os.TempDir(), "some-password.txt")

		result, err := LoadOperatorKeystore(nonExistentFile, passwordFile)
		require.Nil(t, result)
		require.ErrorContains(t, err, "read keystore file")
		require.ErrorContains(t, err, "no such file or directory")
	})

	t.Run("fails when passwordFile does not exist", func(t *testing.T) {
		t.Parallel()

		tmpEncryptedFile := createTempFile(t, "valid-encrypted-", ".json", []byte(`encrypted-content`))
		defer os.Remove(tmpEncryptedFile)

		passwordFile := filepath.Join(os.TempDir(), "nonexistent-password.txt")

		result, err := LoadOperatorKeystore(tmpEncryptedFile, passwordFile)
		require.Nil(t, result)
		require.ErrorContains(t, err, "read password file")
		require.ErrorContains(t, err, "no such file or directory")
	})

	t.Run("fails if password file is empty", func(t *testing.T) {
		t.Parallel()

		tmpEncryptedFile := createTempFile(t, "valid-encrypted-", ".json", []byte(`encrypted-content`))
		defer os.Remove(tmpEncryptedFile)

		tmpEmptyPasswordFile := createTempFile(t, "empty-password-", ".txt", []byte{})
		defer os.Remove(tmpEmptyPasswordFile)

		result, err := LoadOperatorKeystore(tmpEncryptedFile, tmpEmptyPasswordFile)
		require.Nil(t, result)
		require.ErrorContains(t, err, "password file is empty")
	})

	t.Run("fails if DecryptKeystore returns an error", func(t *testing.T) {
		t.Parallel()

		tmpEncryptedFile := createTempFile(t, "invalid-encrypted-", ".json", []byte(`bad-encrypted-data`))
		defer os.Remove(tmpEncryptedFile)

		tmpPasswordFile := createTempFile(t, "valid-password-", ".txt", []byte(`somepassword`))
		defer os.Remove(tmpPasswordFile)

		result, err := LoadOperatorKeystore(tmpEncryptedFile, tmpPasswordFile)
		require.Nil(t, result)
		require.Error(t, err)
		require.Contains(t, err.Error(), "decrypt operator private key keystore")
	})

	t.Run("fails if PrivateKeyFromBytes returns an error", func(t *testing.T) {
		t.Parallel()

		privkey := []byte("privateKey")

		keystore, err := EncryptKeystore(privkey, testPubKeyBase64, testPassword)
		require.NoError(t, err)

		tmpEncryptedFile := createTempFile(t, "bad-for-privkey-", ".json", keystore)
		defer os.Remove(tmpEncryptedFile)

		tmpPasswordFile := createTempFile(t, "valid-password-", ".txt", []byte(testPassword))
		defer os.Remove(tmpPasswordFile)

		result, err := LoadOperatorKeystore(tmpEncryptedFile, tmpPasswordFile)
		require.Nil(t, result)
		require.Error(t, err)
		require.Contains(t, err.Error(), "extract operator private key from keystore")
	})

	t.Run("succeeds with valid files and data", func(t *testing.T) {
		t.Parallel()

		privKey, err := keys.GeneratePrivateKey()
		require.NoError(t, err)

		keystore, err := EncryptKeystore(privKey.Bytes(), testPubKeyBase64, testPassword)
		require.NoError(t, err)

		tmpEncryptedFile := createTempFile(t, "valid-encrypted-", ".json", keystore)
		defer os.Remove(tmpEncryptedFile)

		tmpPasswordFile := createTempFile(t, "valid-password-", ".txt", []byte(testPassword))
		defer os.Remove(tmpPasswordFile)

		result, err := LoadOperatorKeystore(tmpEncryptedFile, tmpPasswordFile)
		require.NoError(t, err, "Should succeed with valid files and correct data")
		require.NotNil(t, result, "Should return a valid OperatorPrivateKey object")
	})
}

func TestGenerateShareKeystore(t *testing.T) {
	t.Parallel()

	t.Run("succeeds with valid BLS key and passphrase", func(t *testing.T) {
		t.Parallel()

		sharePrivateKey := new(bls.SecretKey)
		sharePrivateKey.SetByCSPRNG()

		sharePublicKey := phase0.BLSPubKey{0x12, 0x34, 0x56}
		passphrase := "supersecretpassphrase"

		keystore, err := GenerateShareKeystore(sharePrivateKey, sharePublicKey, passphrase)
		require.NoError(t, err)
		require.NotNil(t, keystore)

		require.Contains(t, keystore, "crypto")
		require.Contains(t, keystore, "pubkey")
		require.Contains(t, keystore, "version")
		require.Contains(t, keystore, "uuid")
		require.Contains(t, keystore, "path")

		require.Equal(t, 4, keystore["version"], "Expected version in keystore to be 4")

		pubkeyVal, ok := keystore["pubkey"].(string)
		require.True(t, ok, "pubkey should be a string")
		require.Equal(t, sharePublicKey.String(), pubkeyVal, "pubkey should match")
	})
}

func createTempFile(t *testing.T, prefix, suffix string, data []byte) string {
	t.Helper()

	tmpFile, err := os.CreateTemp(t.TempDir(), prefix+"*"+suffix)
	require.NoError(t, err, "unable to create temporary file")

	_, writeErr := tmpFile.Write(data)
	require.NoError(t, writeErr, "unable to write to temporary file")

	closeErr := tmpFile.Close()
	require.NoError(t, closeErr, "unable to close temporary file")

	return tmpFile.Name()
}
