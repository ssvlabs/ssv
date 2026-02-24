package domain

import (
	"context"

	"github.com/attestantio/go-eth2-client/api"
)

// NoopUnblinder always returns "no unblinded block".
type NoopUnblinder struct{}

func (u NoopUnblinder) UnblindBlock(_ context.Context, _ *api.VersionedSignedBlindedBeaconBlock) (*api.VersionedSignedProposal, error) {
	return nil, nil
}
