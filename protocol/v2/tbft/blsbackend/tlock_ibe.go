package blsbackend

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/drand/kyber"
	bls12381 "github.com/drand/kyber-bls12381"
	"github.com/drand/kyber/encrypt/ibe"
	"github.com/drand/kyber/pairing"
)

// TLockIBE is a tbft.ThresholdIBE backed by drand's kyber-bls12381 IBE
// primitives (the same primitives drand/tlock uses internally), composed
// with AES-GCM for hybrid encryption.
//
// Why hybrid: kyber's IBE has a plaintext-size limit equal to the hash
// size (32 bytes for SHA-256). TBFT encrypts BLS partial signatures
// (96 bytes for kyber-G2 sigs), which exceeds that. So we IBE-encrypt
// a fresh 32-byte AES key and then AES-GCM-encrypt the actual plaintext
// with that key. Decryption reverses: IBE-decrypt the key, AES-decrypt
// the body.
//
// The decryption key TLockIBE expects (the `key []byte` argument to
// `Decrypt`) is a serialized kyber G2 point — the aggregated BLS
// signature on the IBE tag, produced by `KyberSigner.AggregatePartials`
// over 2f+1 partial sigs. Under Option A's "DST trick" (see
// docs/IBE-INTEGRATION.md), those partial sigs come from operators'
// existing herumi validator shares, reinterpreted as kyber scalars.
//
// The cluster-pubkey TLockIBE expects (the `clusterPubKey []byte`
// argument to `Encrypt`) is the validator's herumi-format G1 pubkey
// (48 bytes), which is byte-equivalent to its kyber-G1 form.
//
// Wire format of the ciphertext output:
//
//	[1] version (0x05)
//	[2] ibe-U-len (uint16 BE; expected 48 for G1)
//	[ibe-U-len] ibe.Ciphertext.U (kyber G1 marshaled)
//	[2] ibe-V-len (uint16 BE)
//	[ibe-V-len] ibe.Ciphertext.V
//	[2] ibe-W-len (uint16 BE)
//	[ibe-W-len] ibe.Ciphertext.W
//	[12] AES-GCM nonce
//	[remainder] AES-GCM ciphertext (plaintext + 16-byte auth tag)
type TLockIBE struct {
	suite pairing.Suite
}

// NewTLockIBE constructs a TLockIBE using kyber-bls12381's default suite.
// The default DSTs match drand's (and what `drand/tlock` uses), so a
// kyber-aggregated BLS sig — including one produced via the DST trick
// from herumi shares — serves as a valid decryption key.
func NewTLockIBE() *TLockIBE {
	return &TLockIBE{suite: bls12381.NewBLS12381Suite()}
}

const tlockIBEVersionV1 byte = 0x05

const (
	aesKeySize      = 32 // AES-256
	aesGCMNonceSize = 12
)

// Encrypt produces a hybrid ciphertext: a fresh AES-256 key wrapped under
// IBE for the (clusterPubKey, tag) identity, followed by AES-GCM ciphertext
// of the plaintext under that AES key.
func (t *TLockIBE) Encrypt(clusterPubKey []byte, tag []byte, plaintext []byte) ([]byte, error) {
	if len(tag) == 0 {
		return nil, errors.New("blsbackend: TLockIBE.Encrypt: empty tag")
	}
	masterPoint, err := HerumiPubkeyToKyberG1Point(clusterPubKey)
	if err != nil {
		return nil, fmt.Errorf("blsbackend: TLockIBE.Encrypt: convert master pubkey: %w", err)
	}

	// 1. Generate a fresh 32-byte AES key.
	aesKey := make([]byte, aesKeySize)
	if _, err := rand.Read(aesKey); err != nil {
		return nil, fmt.Errorf("blsbackend: TLockIBE.Encrypt: read random aes key: %w", err)
	}

	// 2. IBE-encrypt the AES key, identity = tag.
	ibeCipher, err := ibe.EncryptCCAonG1(t.suite, masterPoint, tag, aesKey)
	if err != nil {
		return nil, fmt.Errorf("blsbackend: TLockIBE.Encrypt: ibe encrypt: %w", err)
	}

	// 3. AES-GCM-encrypt the plaintext under aesKey with a fresh random nonce.
	gcm, err := newGCM(aesKey)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aesGCMNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("blsbackend: TLockIBE.Encrypt: read nonce: %w", err)
	}
	// AAD is left empty: the tag is *implicitly* authenticated via IBE —
	// a wrong-tag decryption key produces a garbage AES key, and AES-GCM
	// authentication then fails on the body. So tampering with which tag
	// the ciphertext is "for" is detected by the auth-tag failure on the
	// AES-GCM body, not by an AAD mismatch.
	body := gcm.Seal(nil, nonce, plaintext, nil)

	// 4. Marshal everything to wire bytes.
	uBytes, err := ibeCipher.U.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("blsbackend: TLockIBE.Encrypt: marshal U: %w", err)
	}

	out := make([]byte, 0, 1+2+len(uBytes)+2+len(ibeCipher.V)+2+len(ibeCipher.W)+aesGCMNonceSize+len(body))
	out = append(out, tlockIBEVersionV1)
	out = appendU16(out, len(uBytes))
	out = append(out, uBytes...)
	out = appendU16(out, len(ibeCipher.V))
	out = append(out, ibeCipher.V...)
	out = appendU16(out, len(ibeCipher.W))
	out = append(out, ibeCipher.W...)
	out = append(out, nonce...)
	out = append(out, body...)
	return out, nil
}

// Decrypt reverses Encrypt: parses the wire bytes, IBE-decrypts the wrapped
// AES key using the supplied threshold-BLS sig as the IBE decryption key,
// then AES-GCM-decrypts the body.
//
// `key` must be a kyber-G2-marshaled BLS signature on the same tag the
// ciphertext is bound to (i.e. the aggregate of 2f+1 KyberSigner partial
// sigs over the tag).
func (t *TLockIBE) Decrypt(ciphertext []byte, key []byte) ([]byte, error) {
	r := newWireReader(ciphertext)

	version, err := r.readByte("version")
	if err != nil {
		return nil, err
	}
	if version != tlockIBEVersionV1 {
		return nil, fmt.Errorf("blsbackend: TLockIBE: unsupported ciphertext version 0x%02x", version)
	}

	uLen, err := r.readU16("ibe-U length")
	if err != nil {
		return nil, err
	}
	uBytes, err := r.readBytes(int(uLen), "ibe-U")
	if err != nil {
		return nil, err
	}
	vLen, err := r.readU16("ibe-V length")
	if err != nil {
		return nil, err
	}
	vBytes, err := r.readBytes(int(vLen), "ibe-V")
	if err != nil {
		return nil, err
	}
	wLen, err := r.readU16("ibe-W length")
	if err != nil {
		return nil, err
	}
	wBytes, err := r.readBytes(int(wLen), "ibe-W")
	if err != nil {
		return nil, err
	}
	nonce, err := r.readBytes(aesGCMNonceSize, "aes-gcm nonce")
	if err != nil {
		return nil, err
	}
	body, err := r.readRest()
	if err != nil {
		return nil, err
	}

	// Reconstruct the kyber IBE Ciphertext.
	uPoint := bls12381.NullKyberG1()
	if err := uPoint.UnmarshalBinary(uBytes); err != nil {
		return nil, fmt.Errorf("blsbackend: TLockIBE: unmarshal U: %w", err)
	}
	ibeCipher := &ibe.Ciphertext{U: uPoint, V: vBytes, W: wBytes}

	// Parse the threshold-BLS sig (G2 point) we'll use as the IBE decryption key.
	sigPoint := bls12381.NullKyberG2()
	if err := sigPoint.UnmarshalBinary(key); err != nil {
		return nil, fmt.Errorf("blsbackend: TLockIBE: unmarshal sig key as kyber G2: %w", err)
	}

	// IBE-decrypt the wrapped AES key.
	aesKey, err := ibe.DecryptCCAonG1(t.suite, sigPoint, ibeCipher)
	if err != nil {
		return nil, fmt.Errorf("blsbackend: TLockIBE: ibe decrypt: %w", err)
	}

	// AES-GCM-decrypt the body. The tag-binding is enforced via IBE: a
	// wrong key produces a garbage AES key, which fails AES-GCM auth.
	gcm, err := newGCM(aesKey)
	if err != nil {
		return nil, err
	}
	plaintext, err := gcm.Open(nil, nonce, body, nil)
	if err != nil {
		return nil, fmt.Errorf("blsbackend: TLockIBE: aes-gcm open: %w", err)
	}
	return plaintext, nil
}

// ---- helpers ------------------------------------------------------------

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("blsbackend: TLockIBE: aes new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("blsbackend: TLockIBE: aes-gcm: %w", err)
	}
	return gcm, nil
}

func appendU16(b []byte, v int) []byte {
	if v < 0 || v > 0xFFFF {
		// caller is responsible — this is an internal helper called from
		// Encrypt where lengths are bounded by IBE primitive output size.
		panic(fmt.Sprintf("blsbackend: TLockIBE: u16 overflow: %d", v))
	}
	var buf [2]byte
	binary.BigEndian.PutUint16(buf[:], uint16(v)) //nolint:gosec // bounds-checked above
	return append(b, buf[:]...)
}

type wireReader struct {
	data []byte
	pos  int
}

func newWireReader(d []byte) *wireReader { return &wireReader{data: d} }

func (r *wireReader) remaining() int { return len(r.data) - r.pos }

func (r *wireReader) readByte(name string) (byte, error) {
	if r.remaining() < 1 {
		return 0, fmt.Errorf("blsbackend: TLockIBE: truncated reading %s", name)
	}
	b := r.data[r.pos]
	r.pos++
	return b, nil
}

func (r *wireReader) readU16(name string) (uint16, error) {
	if r.remaining() < 2 {
		return 0, fmt.Errorf("blsbackend: TLockIBE: truncated reading %s", name)
	}
	v := binary.BigEndian.Uint16(r.data[r.pos : r.pos+2])
	r.pos += 2
	return v, nil
}

func (r *wireReader) readBytes(n int, name string) ([]byte, error) {
	if n < 0 || r.remaining() < n {
		return nil, fmt.Errorf("blsbackend: TLockIBE: truncated reading %s (need %d, have %d)",
			name, n, r.remaining())
	}
	out := make([]byte, n)
	copy(out, r.data[r.pos:r.pos+n])
	r.pos += n
	return out, nil
}

func (r *wireReader) readRest() ([]byte, error) {
	if r.remaining() <= 0 {
		return nil, errors.New("blsbackend: TLockIBE: empty body")
	}
	out := make([]byte, r.remaining())
	copy(out, r.data[r.pos:])
	r.pos = len(r.data)
	return out, nil
}

// Compile-time assertion that bls12381.NullKyberG1() satisfies kyber.Point —
// guards against silent breakage if the kyber-bls12381 dependency changes
// its type returns.
var _ kyber.Point = bls12381.NullKyberG1()
