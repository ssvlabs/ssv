package ssvsigner

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

type ShareDecryptionError error

type remoteSigner interface {
	ListKeys(ctx context.Context) ([]phase0.BLSPubKey, error)
	ImportKeystore(ctx context.Context, keystoreList []web3signer.Keystore, keystorePasswordList []string) ([]web3signer.Status, error)
	DeleteKeystore(ctx context.Context, sharePubKeyList []phase0.BLSPubKey) ([]web3signer.Status, error)
	Sign(ctx context.Context, sharePubKey phase0.BLSPubKey, payload web3signer.SignRequest) (phase0.BLSSignature, error)
}

type ListValidatorsResponse []phase0.BLSPubKey

type AddValidatorRequest struct {
	ShareKeys []ShareKeys `json:"share_keys"`
}

type ShareKeys struct {
	EncryptedPrivKey hexutil.Bytes
	PublicKey        phase0.BLSPubKey
}

type AddValidatorResponse struct {
	Statuses []web3signer.Status
}
type RemoveValidatorRequest struct {
	PublicKeys []phase0.BLSPubKey `json:"public_keys"`
}

type RemoveValidatorResponse struct {
	Statuses []web3signer.Status `json:"statuses"`
}
