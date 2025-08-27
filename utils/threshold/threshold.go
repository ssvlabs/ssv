package threshold

import (
	"fmt"

	"github.com/herumi/bls-eth-go-binary/bls"
)

// Create receives a bls.SecretKey hex and count.
// Will split the secret key into count shares
func Create(skBytes []byte, threshold uint64, count uint64) (map[uint64]*bls.SecretKey, error) {

	// Validate threshold parameter - must be at least 2 for meaningful threshold schemes
	if threshold <= 1 {
		return nil, fmt.Errorf("invalid threshold: threshold must be greater than 1, got %d", threshold)
	}

	// Validate that we have enough shares for the threshold
	if count < threshold {
		return nil, fmt.Errorf("insufficient count: need at least %d shares for threshold %d, got %d", threshold, threshold, count)
	}

	// master key Polynomial
	msk := make([]bls.SecretKey, threshold)

	sk := &bls.SecretKey{}
	if err := sk.Deserialize(skBytes); err != nil {
		return nil, err
	}
	msk[0] = *sk

	// construct poly
	for i := uint64(1); i < threshold; i++ {
		sk := bls.SecretKey{}
		sk.SetByCSPRNG()
		msk[i] = sk
	}

	// evaluate shares - starting from 1 because 0 is master key
	shares := make(map[uint64]*bls.SecretKey)
	for i := uint64(1); i <= count; i++ {
		blsID := bls.ID{}

		err := blsID.SetDecString(fmt.Sprintf("%d", i))
		if err != nil {
			return nil, err
		}

		sk := bls.SecretKey{}

		err = sk.Set(msk, &blsID)
		if err != nil {
			return nil, err
		}

		shares[i] = &sk
	}
	return shares, nil
}
