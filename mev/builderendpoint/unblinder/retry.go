package unblinder

import (
	"context"
	"time"

	builderapi "github.com/attestantio/go-builder-client/api"
	eth2api "github.com/attestantio/go-eth2-client/api"
)

func unblindWithRetries(
	ctx context.Context,
	provider UnblindProvider,
	proposal *eth2api.VersionedSignedBlindedProposal,
	retries int,
	retryInterval time.Duration,
) *eth2api.VersionedSignedProposal {
	attempts := 1 + retries
	if attempts < 1 {
		attempts = 1
	}

	for i := 0; i < attempts; i++ {
		resp, err := provider.UnblindProposal(ctx, &builderapi.UnblindProposalOpts{Proposal: proposal})
		if err == nil && resp != nil && resp.Data != nil {
			return resp.Data
		}
		if retryInterval <= 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(retryInterval):
		}
	}

	return nil
}
