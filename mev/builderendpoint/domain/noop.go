package domain

import (
	"context"

	builderspec "github.com/attestantio/go-builder-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// NoopBidProvider always returns "no bid".
type NoopBidProvider struct{}

func (p NoopBidProvider) BuilderBid(_ context.Context, _ phase0.Slot, _ phase0.Hash32, _ phase0.BLSPubKey) (*builderspec.VersionedSignedBuilderBid, error) {
	return nil, nil
}
