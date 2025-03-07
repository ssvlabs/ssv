package ssvsigner

import (
	"context"

	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

type ShareDecryptionError error

type remoteSigner interface {
	ListKeys(ctx context.Context) ([]string, error)
	ImportKeystore(ctx context.Context, keystoreList, keystorePasswordList []string) ([]web3signer.Status, error)
	DeleteKeystore(ctx context.Context, sharePubKeyList []string) ([]web3signer.Status, error)
	Sign(ctx context.Context, sharePubKey []byte, payload web3signer.SignRequest) ([]byte, error)
}

type ClientShareKeys struct {
	EncryptedPrivKey []byte
	PublicKey        []byte
}

type ListValidatorsResponse []string

type AddValidatorRequest struct {
	ShareKeys []ServerShareKeys `json:"share_keys"`
}

type ServerShareKeys struct {
	EncryptedPrivKey string `json:"encrypted_private_key"`
	PublicKey        string `json:"public_key"`
}

type AddValidatorResponse struct {
	Statuses []web3signer.Status
}
type RemoveValidatorRequest struct {
	PublicKeys []string `json:"public_keys"`
}

type RemoveValidatorResponse struct {
	Statuses []web3signer.Status `json:"statuses"`
}
