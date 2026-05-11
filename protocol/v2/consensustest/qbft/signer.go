package qbft

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"sync"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// virtualOperatorSigner signs SSVMessages with the operator's RSA private key.
// Production signs the same way (operator's RSA key for outer-envelope auth);
// using real RSA keeps qbft.Instance's signature verification path live in our
// sims without bypass.
//
// Signatures are cached via signRSAWithCache — PKCS#1 v1.5 RSA is
// deterministic, so caching the result is safe and saves the ~1 ms RSA-sign
// cost on the hot PROPOSE/PREPARE/COMMIT path.
type virtualOperatorSigner struct {
	op spectypes.OperatorID
	sk *rsa.PrivateKey
}

func newVirtualOperatorSigner(op spectypes.OperatorID, sk *rsa.PrivateKey) *virtualOperatorSigner {
	return &virtualOperatorSigner{op: op, sk: sk}
}

func (s *virtualOperatorSigner) SignSSVMessage(msg *spectypes.SSVMessage) ([]byte, error) {
	return signRSAWithCache(s.sk, msg)
}

func (s *virtualOperatorSigner) GetOperatorID() spectypes.OperatorID {
	return s.op
}

// rsaSignatureCache holds the (skFingerprint, message-digest) → signature
// mapping. RSA PKCS#1 v1.5 with SHA-256 is deterministic in (sk, digest),
// so the same key signing the same canonical message always produces the
// same signature. Across iterations of a scenario, honest PROPOSE/PREPARE/
// COMMIT shapes are identical (same height, round, value roots), so cache
// hit rate runs ~99% after the first iteration.
//
// Keyed on the public-key fingerprint rather than the OperatorID because
// the same OperatorID (e.g., 1) can hold different RSA keys across the
// Testing{4,7,10,13}SharesSet KeySets — caching by OperatorID alone would
// return signatures from the wrong key when sub-tests run in parallel.
//
// Memory cost is negligible: ~256 B per cached signature; even with
// thousands of distinct messages across a stress run, the cache fits in a
// few hundred KB.
var rsaSignatureCache sync.Map // rsaCacheKey → []byte (signature)

// rsaCacheKey is the cache key: 32-byte SHA-256 of the public modulus
// concatenated with the 32-byte SHA-256 digest of the encoded SSVMessage.
// Fixed-size array so sync.Map's comparable-key requirement is satisfied
// without hashing strings.
type rsaCacheKey [64]byte

// signRSAWithCache returns the (cached or freshly computed) PKCS#1 v1.5
// signature over msg's SSZ encoding using the given private key. The cache
// key uses a SHA-256 fingerprint of the public modulus so distinct keys
// (even at the same OperatorID in different test KeySets) get distinct
// cache entries. Fingerprint computation is ~5µs vs RSA sign ~1 ms — the
// cache hit path is still ~100× faster than a fresh sign.
func signRSAWithCache(sk *rsa.PrivateKey, msg *spectypes.SSVMessage) ([]byte, error) {
	encoded, err := msg.Encode()
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(encoded)
	fingerprint := sha256.Sum256(sk.PublicKey.N.Bytes())
	var key rsaCacheKey
	copy(key[:32], fingerprint[:])
	copy(key[32:], digest[:])
	if cached, ok := rsaSignatureCache.Load(key); ok {
		return cached.([]byte), nil
	}
	sig, err := rsa.SignPKCS1v15(rand.Reader, sk, crypto.SHA256, digest[:])
	if err != nil {
		return nil, err
	}
	rsaSignatureCache.Store(key, sig)
	return sig, nil
}
