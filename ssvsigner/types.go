package ssvsigner

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

type ShareDecryptionError error

type RemoteSigner interface {
	ListKeys(ctx context.Context) (web3signer.ListKeysResponse, error)
	ImportKeystore(ctx context.Context, req web3signer.ImportKeystoreRequest) (web3signer.ImportKeystoreResponse, error)
	DeleteKeystore(ctx context.Context, req web3signer.DeleteKeystoreRequest) (web3signer.DeleteKeystoreResponse, error)
	Sign(ctx context.Context, sharePubKey phase0.BLSPubKey, req web3signer.SignRequest) (web3signer.SignResponse, error)
}

type AddValidatorRequest struct {
	ShareKeys []ShareKeys `json:"share_keys"`
}

type ShareKeys struct {
	EncryptedPrivKey hexutil.Bytes
	PubKey           phase0.BLSPubKey
}
