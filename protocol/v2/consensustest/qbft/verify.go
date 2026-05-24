package qbft

import (
	"crypto/sha256"
	"encoding/binary"
	"sync"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	qbftcfg "github.com/ssvlabs/ssv/protocol/v2/qbft"
)

// verifyCache memoizes consensus-message signature-verification results across
// the entire stress run. The sweep verifies the same signatures an enormous
// number of times: a cell's proposed value, identifier and height are fixed
// across its iterations, so round-1 PROPOSE / PREPARE / ROUND-CHANGE signatures
// are byte-identical every iteration, and within a single sim QBFT re-validates
// the accumulated round-change justifications O(n²) times as each new
// round-change arrives. RSA-2048 verification dominated CPU (~30% of the run);
// a cache hit replaces the modexp with a SHA-256 + map lookup.
//
// Correctness: spectypes.Verify(msg, operators) is a pure function of
// (operators' public keys, sha256(msg.SSVMessage), msg.OperatorIDs,
// msg.Signatures) — see ssv-spec types.Verify. The Tier-1 keyset cache makes
// each cluster size's keyset a fixed process-wide singleton, so the cluster
// size n uniquely identifies the operator public keys. The key is therefore
// (n, msgHash, signers, signatures) and cannot collide across cluster sizes,
// and the stored value is the exact error spectypes.Verify returned, so every
// outcome (including failure modes) is identical to the uncached path.
//
// The distinct-key set stays small (bounded by the few distinct signed
// (round, root, signer) tuples a fixed-value sim produces), so the cache does
// not grow without bound over a long run.
var verifyCache sync.Map // [32]byte -> verifyResult

type verifyResult struct{ err error }

// newCachingVerifier returns a SignatureVerifier for cluster size n that
// consults verifyCache, falling back to spectypes.Verify on a miss. Messages
// that fail to encode are never cached — the fallback surfaces the same error.
func newCachingVerifier(n int) qbftcfg.SignatureVerifier {
	return func(msg *spectypes.SignedSSVMessage, operators []*spectypes.Operator) error {
		key, ok := verifyKey(n, msg)
		if !ok {
			return spectypes.Verify(msg, operators)
		}
		if v, hit := verifyCache.Load(key); hit {
			return v.(verifyResult).err
		}
		err := spectypes.Verify(msg, operators)
		verifyCache.Store(key, verifyResult{err: err})
		return err
	}
}

// verifyKey builds the (n, sha256(SSVMessage), signers, signatures) cache key.
// Counts and per-signature length prefixes make the serialization unambiguous.
// Returns ok=false when the SSVMessage can't be encoded, so the caller defers
// to spectypes.Verify (which will report the same encode error).
func verifyKey(n int, msg *spectypes.SignedSSVMessage) ([32]byte, bool) {
	if msg == nil || msg.SSVMessage == nil {
		return [32]byte{}, false
	}
	encoded, err := msg.SSVMessage.Encode()
	if err != nil {
		return [32]byte{}, false
	}
	msgHash := sha256.Sum256(encoded)

	h := sha256.New()
	var scratch [8]byte
	put := func(v uint64) {
		binary.BigEndian.PutUint64(scratch[:], v)
		h.Write(scratch[:])
	}
	put(uint64(n))
	h.Write(msgHash[:])
	put(uint64(len(msg.OperatorIDs)))
	for _, id := range msg.OperatorIDs {
		put(uint64(id))
	}
	put(uint64(len(msg.Signatures)))
	for _, sig := range msg.Signatures {
		put(uint64(len(sig)))
		h.Write(sig)
	}
	var key [32]byte
	h.Sum(key[:0])
	return key, true
}
