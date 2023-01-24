package bootnode

import (
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
)

type ENRForkID struct {
	CurrentForkDigest []byte `ssz-size:"4"`
	NextForkVersion   []byte `ssz-size:"4"`
	NextForkEpoch     spec.Epoch
}
