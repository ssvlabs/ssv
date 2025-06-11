package ssvsigner

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

type ShareDecryptionError error

type AddValidatorRequest struct {
	ShareKeys []ShareKeys `json:"share_keys"`
}

type ShareKeys struct {
	EncryptedPrivKey hexutil.Bytes
	PubKey           phase0.BLSPubKey
}
