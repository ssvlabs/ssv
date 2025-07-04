package mocks

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

var (
	_ keys.OperatorPublicKey  = (*TestOperatorPublicKey)(nil)
	_ keys.OperatorPrivateKey = (*TestOperatorPrivateKey)(nil)
	_ web3signer.RemoteSigner = (*TestRemoteSigner)(nil)
)

// TestOperatorPublicKey implements a mock operator public key for testing.
type TestOperatorPublicKey struct {
	PubKeyBase64 string
	Base64Error  error
}

// Encrypt mocks encryption with the public key.
func (t *TestOperatorPublicKey) Encrypt(data []byte) ([]byte, error) {
	return data, nil
}

// Verify mocks signature verification.
func (t *TestOperatorPublicKey) Verify([]byte, []byte) error {
	return nil
}

// Base64 returns the public key as a base64 string.
func (t *TestOperatorPublicKey) Base64() (string, error) {
	return t.PubKeyBase64, t.Base64Error
}

// TestOperatorPrivateKey implements a mock operator private key for testing.
type TestOperatorPrivateKey struct {
	Base64Value           string
	BytesValue            []byte
	StorageHashValue      []byte
	EkmHashValue          []byte
	EkmEncryptionKeyValue []byte
	DecryptResult         []byte
	DecryptError          error
	SignResult            []byte
	SignError             error
	PublicKey             keys.OperatorPublicKey
}

// Sign mocks signing data with the private key.
func (t *TestOperatorPrivateKey) Sign([]byte) ([]byte, error) {
	if t.SignError != nil {
		return nil, t.SignError
	}

	return t.SignResult, nil
}

// Public returns the public key.
func (t *TestOperatorPrivateKey) Public() keys.OperatorPublicKey {
	return t.PublicKey
}

// Decrypt mocks decryption of encrypted data.
func (t *TestOperatorPrivateKey) Decrypt([]byte) ([]byte, error) {
	if t.DecryptError != nil {
		return nil, t.DecryptError
	}

	return t.DecryptResult, nil
}

// StorageHash returns a mock storage hash.
func (t *TestOperatorPrivateKey) StorageHash() []byte {
	return t.StorageHashValue
}

// EKMHash returns a mock EKM hash.
func (t *TestOperatorPrivateKey) EKMHash() []byte {
	return t.EkmHashValue
}

// EKMEncryptionKey returns a mock EKM encryption key.
func (t *TestOperatorPrivateKey) EKMEncryptionKey() ([]byte, error) {
	return t.EkmEncryptionKeyValue, nil
}

// Bytes returns the private key bytes.
func (t *TestOperatorPrivateKey) Bytes() []byte {
	return t.BytesValue
}

// Base64 returns the private key as a base64 string.
func (t *TestOperatorPrivateKey) Base64() string {
	return t.Base64Value
}

// TestRemoteSigner implements a mock remote signer for testing.
type TestRemoteSigner struct {
	ListKeysResult []phase0.BLSPubKey
	ListKeysError  error
	ImportResult   web3signer.ImportKeystoreResponse
	ImportError    error
	DeleteResult   web3signer.DeleteKeystoreResponse
	DeleteError    error
	SignResult     web3signer.SignResponse
	SignError      error
}

// ListKeys mocks listing keys from the remote signer.
func (t *TestRemoteSigner) ListKeys(context.Context) (web3signer.ListKeysResponse, error) {
	if t.ListKeysError != nil {
		return nil, t.ListKeysError
	}

	return t.ListKeysResult, nil
}

// ImportKeystore mocks importing a keystore to the remote signer.
func (t *TestRemoteSigner) ImportKeystore(context.Context, web3signer.ImportKeystoreRequest) (web3signer.ImportKeystoreResponse, error) {
	if t.ImportError != nil {
		return web3signer.ImportKeystoreResponse{}, t.ImportError
	}

	return t.ImportResult, nil
}

// DeleteKeystore mocks deleting a keystore from the remote signer.
func (t *TestRemoteSigner) DeleteKeystore(context.Context, web3signer.DeleteKeystoreRequest) (web3signer.DeleteKeystoreResponse, error) {
	if t.DeleteError != nil {
		return web3signer.DeleteKeystoreResponse{}, t.DeleteError
	}

	return t.DeleteResult, nil
}

// Sign mocks signing with the remote signer.
func (t *TestRemoteSigner) Sign(context.Context, phase0.BLSPubKey, web3signer.SignRequest) (web3signer.SignResponse, error) {
	if t.SignError != nil {
		return web3signer.SignResponse{}, t.SignError
	}
	return t.SignResult, nil
}
