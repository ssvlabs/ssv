package ssvsigner

import (
	"errors"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

type ShareDecryptionError error

var ErrOperatorDataProtectionUnsupported = errors.New("operator data protection is unsupported by this ssv-signer")

type AddValidatorRequest struct {
	ShareKeys []ShareKeys `json:"share_keys"`
}

type ShareKeys struct {
	EncryptedPrivKey hexutil.Bytes
	PubKey           phase0.BLSPubKey
}
