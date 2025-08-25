package common

import (
	"encoding/hex"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"

	"github.com/ssvlabs/ssv/ssvsigner/keys"
)

// Initialize BLS library once when the package is imported
func init() {
	if err := initializeBLS(); err != nil {
		panic(fmt.Sprintf("Failed to initialize BLS library: %v", err))
	}
}

// ValidatorKeyPair contains keys for a validator
type ValidatorKeyPair struct {
	BLSPubKey      phase0.BLSPubKey // 48-byte BLS public key for Ethereum
	EncryptedShare []byte           // RSA-encrypted private key share for SSV
}

// GenerateOperatorKey generates an RSA operator key
func GenerateOperatorKey() (keys.OperatorPrivateKey, error) {
	operatorPrivKey, err := keys.GeneratePrivateKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate operator key: %w", err)
	}
	return operatorPrivKey, nil
}

// GenerateValidatorShare generates a BLS validator key and encrypts it with the operator's RSA public key
func GenerateValidatorShare(operatorKey keys.OperatorPrivateKey) (*ValidatorKeyPair, error) {
	// Generate BLS key pair
	secretKey := &bls.SecretKey{}
	secretKey.SetByCSPRNG()

	publicKey := secretKey.GetPublicKey()

	// Convert keys to required formats
	privateKeyBytes := secretKey.Serialize()
	publicKeyBytes := publicKey.Serialize()

	privateKeyHex := hex.EncodeToString(privateKeyBytes)

	// Create phase0.BLSPubKey with proper validation
	var blsPubKey phase0.BLSPubKey
	if len(publicKeyBytes) != len(blsPubKey) {
		return nil, fmt.Errorf("invalid public key length: expected %d, got %d", len(blsPubKey), len(publicKeyBytes))
	}
	copy(blsPubKey[:], publicKeyBytes)

	// Encrypt the private key with operator's RSA public key
	encryptedShare, err := operatorKey.Public().Encrypt([]byte(privateKeyHex))
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt validator share: %w", err)
	}

	return &ValidatorKeyPair{
		BLSPubKey:      blsPubKey,
		EncryptedShare: encryptedShare,
	}, nil
}

// initializeBLS initializes the BLS library with proper settings for Ethereum
func initializeBLS() error {
	if err := bls.Init(bls.BLS12_381); err != nil {
		return fmt.Errorf("failed to initialize BLS12-381 curve: %w", err)
	}
	if err := bls.SetETHmode(bls.EthModeDraft07); err != nil {
		return fmt.Errorf("failed to set Ethereum mode (draft-07): %w", err)
	}
	return nil
}
