package keystore

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDecryptKeystoreWithInvalidData(t *testing.T) {
	password := "password"
	encryptedJSONData := []byte(`{"version":4,"pubKey":"base64EncodedPublicKey","crypto":{"kdf":"scrypt","checksum":{"function":"sha256","params":{"dklen":32,"salt":"base64EncodedSalt"},"message":"base64EncodedMessage"},"cipher":{"function":"aes-128-ctr","params":{"iv":"base64EncodedIV"},"message":"base64EncodedEncryptedMessage"},"kdfparams":{"n":262144,"r":8,"p":1,"salt":"base64EncodedSalt"}}}`)
	_, err := DecryptKeystore(encryptedJSONData, password)
	require.Error(t, err)
}

func TestDecryptKeystoreWithEmptyPassword(t *testing.T) {
	password := ""
	encryptedJSONData := []byte(`{"valid":"data"}`)
	_, err := DecryptKeystore(encryptedJSONData, password)
	require.NotNil(t, err)
}

func TestEncryptKeystoreWithValidData(t *testing.T) {
	password := "password"
	privkey := []byte("privateKey")
	pubKeyBase64 := "base64EncodedPublicKey"
	data, err := EncryptKeystore(privkey, pubKeyBase64, password)
	require.Nil(t, err)
	var jsonData map[string]interface{}
	err = json.Unmarshal(data, &jsonData)
	require.Nil(t, err)
	require.Equal(t, pubKeyBase64, jsonData["pubKey"])

	decrtypted, err := DecryptKeystore(data, password)
	require.Nil(t, err)
	require.Equal(t, privkey, decrtypted)
}

func TestEncryptKeystoreWithEmptyPassword(t *testing.T) {
	password := ""
	privkey := []byte("privateKey")
	pubKeyBase64 := "base64EncodedPublicKey"
	_, err := EncryptKeystore(privkey, pubKeyBase64, password)
	require.NotNil(t, err)
}
