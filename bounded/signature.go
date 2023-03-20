package bounded

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/cornelk/hashmap"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
)

var pkCache = hashmap.New[string, bls.PublicKey]()

// VerifyByOperators verifies signature by the provided operators
func VerifyByOperators(s spectypes.Signature, data spectypes.MessageSignature, domain spectypes.DomainType, sigType spectypes.SignatureType, operators []*spectypes.Operator) error {
	// decode sig
	sign := &bls.Sign{}
	if err := sign.Deserialize(s); err != nil {
		return errors.Wrap(err, "failed to deserialize signature")
	}

	// runtime.Gosched()

	// find operators
	pks := make([]bls.PublicKey, 0)
	for _, id := range data.GetSigners() {
		found := false
		for _, n := range operators {
			if id == n.GetID() {
				pkStr := string(n.GetPublicKey())
				if pk, ok := pkCache.Get(pkStr); ok {
					pks = append(pks, pk)
					found = true
					continue
				}

				pk := bls.PublicKey{}
				if err := pk.Deserialize(n.GetPublicKey()); err != nil {
					return errors.Wrap(err, "failed to deserialize public key")
				}
				pkCache.Set(pkStr, pk)

				// runtime.Gosched()

				pks = append(pks, pk)
				found = true
			}
		}
		if !found {
			return errors.New("unknown signer")
		}
	}

	// compute root
	computedRoot, err := spectypes.ComputeSigningRoot(data, spectypes.ComputeSignatureDomain(domain, sigType))
	if err != nil {
		return errors.Wrap(err, "could not compute signing root")
	}

	// verify
	if res := sign.FastAggregateVerify(pks, computedRoot); !res {
		return errors.New("failed to verify signature")
	}

	_ = computedRoot
	return nil
}
