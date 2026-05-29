package validation

import (
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"slices"

	"github.com/jellydator/ttlcache/v3"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft/base"
	twoabcore "github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/ssv/runner/obft"
	twoabadapter "github.com/ssvlabs/ssv/protocol/v2/ssv/runner/obft/twoab"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// Per-validator caching of the OBFT / 2abOBFT message-validation Verifier.
//
// Without this, validateOBFTMessage / validateTwoabMessage construct a fresh
// Verifier via NewVerifierFromShare on EVERY inbound envelope. That defeats
// the F2 (proposer signing-root) and F3 (kyber pubkey-parse) caches, which
// only help when the underlying signer instances persist across calls — a
// per-envelope Verifier throws them away each time. The validation layer
// sees every gossiped envelope for every tracked validator, so re-paying
// those cold caches per envelope is the dominant redundant cost on the
// validation hot path.
//
// SECURITY: the Verifier snapshots the validator's committee pub-shares +
// cluster pubkey. A stale entry would verify inbound consensus messages
// against the WRONG shares (accepting sigs from operators no longer in the
// committee, or rejecting newly-added ones). Correctness is guaranteed by a
// content fingerprint re-derived from the live share on every lookup: a
// committee/pub-share change flips the fingerprint and forces a rebuild,
// independent of how the share is mutated upstream (remove+re-add producing
// a new pointer, or any hypothetical in-place mutation). The TTL only bounds
// memory; it is NOT relied on for correctness.

// cachedOBFTVerifier pairs a built OBFT Verifier with the fingerprint of the
// share it was built from. A lookup reuses verifier only when the live
// share's fingerprint still matches.
type cachedOBFTVerifier struct {
	fingerprint [32]byte
	verifier    *obftcore.Verifier
}

// cachedTwoabVerifier is the 2abOBFT twin of cachedOBFTVerifier.
type cachedTwoabVerifier struct {
	fingerprint [32]byte
	verifier    *twoabcore.Verifier
}

// shareVerifierFingerprint hashes exactly the share fields a Verifier depends
// on: each committee member's (Signer, SharePubKey) plus the ValidatorPubKey
// (= cluster pubkey). Committee members are sorted by Signer first because the
// stored committee order is not guaranteed canonical (cf. ComputeCommitteeID,
// which also sorts) — so the fingerprint must be order-independent.
//
// NOTE: the validation layer constructs Verifiers with ibePubKeyShares = nil
// (Option A: V-shares double as IBE shares via DST separation). The NR-side
// pub-shares are therefore derived from the same committee already covered
// here. If Option B (a separate IBE keypair) is ever wired into the
// validation layer, this fingerprint MUST be extended to include the IBE
// shares, or a reshare of the IBE keypair could be served a stale Verifier.
func shareVerifierFingerprint(share *spectypes.Share) [32]byte {
	members := make([]*spectypes.ShareMember, len(share.Committee))
	copy(members, share.Committee)
	slices.SortFunc(members, func(a, b *spectypes.ShareMember) int {
		return cmp.Compare(a.Signer, b.Signer)
	})

	h := sha256.New()
	var idbuf [8]byte
	for _, m := range members {
		binary.BigEndian.PutUint64(idbuf[:], uint64(m.Signer))
		h.Write(idbuf[:])
		h.Write(m.SharePubKey)
	}
	h.Write(share.ValidatorPubKey[:])

	var out [32]byte
	h.Sum(out[:0])
	return out
}

// obftVerifierFor returns a Verifier for the validator's share, reusing a
// cached one when the share's fingerprint is unchanged and building (then
// caching) a fresh one otherwise. The returned Verifier may be shared across
// concurrent validation goroutines; that is safe because its F2 / F3 signer
// sub-caches are mutex-guarded and its PubKeyShares / ClusterPubKey are
// read-only after construction.
func (mv *messageValidator) obftVerifierFor(share *ssvtypes.SSVShare) (*obftcore.Verifier, error) {
	if mv.obftVerifiers == nil {
		// Cache not initialised (a messageValidator built without New() — e.g.
		// in tests). The cache is a pure optimisation; correctness never
		// depends on it, so fall back to direct construction. Mirrors the
		// consensusAdmissions nil-guard pattern elsewhere in this package.
		return obftadapter.NewVerifierFromShare(&share.Share, nil, mv.netCfg.Beacon)
	}

	key := string(share.ValidatorPubKey[:])
	fp := shareVerifierFingerprint(&share.Share)

	if item := mv.obftVerifiers.Get(key); item != nil {
		if cv := item.Value(); cv != nil && cv.fingerprint == fp {
			return cv.verifier, nil
		}
		// Fingerprint mismatch → the committee/pub-shares changed since this
		// entry was cached. Fall through to rebuild; the Set below overwrites
		// the stale entry.
	}

	verifier, err := obftadapter.NewVerifierFromShare(&share.Share, nil /* Option A */, mv.netCfg.Beacon)
	if err != nil {
		return nil, err
	}
	mv.obftVerifiers.Set(key, &cachedOBFTVerifier{fingerprint: fp, verifier: verifier}, ttlcache.DefaultTTL)
	return verifier, nil
}

// twoabVerifierFor is the 2abOBFT twin of obftVerifierFor.
func (mv *messageValidator) twoabVerifierFor(share *ssvtypes.SSVShare) (*twoabcore.Verifier, error) {
	if mv.twoabVerifiers == nil {
		// See obftVerifierFor — graceful degradation when the cache is absent.
		return twoabadapter.NewVerifierFromShare(&share.Share, nil, mv.netCfg.Beacon)
	}

	key := string(share.ValidatorPubKey[:])
	fp := shareVerifierFingerprint(&share.Share)

	if item := mv.twoabVerifiers.Get(key); item != nil {
		if cv := item.Value(); cv != nil && cv.fingerprint == fp {
			return cv.verifier, nil
		}
	}

	verifier, err := twoabadapter.NewVerifierFromShare(&share.Share, nil /* Option A */, mv.netCfg.Beacon)
	if err != nil {
		return nil, err
	}
	mv.twoabVerifiers.Set(key, &cachedTwoabVerifier{fingerprint: fp, verifier: verifier}, ttlcache.DefaultTTL)
	return verifier, nil
}
