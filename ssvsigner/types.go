package ssvsigner

import (
	"errors"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// ShareDecryptionError marks an encrypted validator share that can't be turned into a signing
// key — undecryptable, bad hex, or a share-pubkey mismatch. It signals a malformed share (bad
// on-chain data) rather than a transient/transport failure, so callers can skip the event
// instead of treating it as fatal.
//
// It must stay a concrete type and be returned by value: callers match it with
// errors.As(err, &ShareDecryptionError{}), which an interface alias of error would defeat (matching
// every error) and which a pointer return (&ShareDecryptionError{}) would slip past unmatched.
type ShareDecryptionError struct {
	Err error
}

func (e ShareDecryptionError) Error() string {
	if e.Err == nil {
		return "share decryption error: nil"
	}
	return "share decryption error: " + e.Err.Error()
}

func (e ShareDecryptionError) Unwrap() error {
	return e.Err
}

var ErrOperatorDataProtectionUnsupported = errors.New("operator data protection is unsupported by this ssv-signer")

type AddValidatorRequest struct {
	ShareKeys []ShareKeys `json:"share_keys"`
}

type ShareKeys struct {
	EncryptedPrivKey hexutil.Bytes
	PubKey           phase0.BLSPubKey
}
