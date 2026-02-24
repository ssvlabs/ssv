package unblinder_test

import (
	"context"
	"errors"
	"testing"
	"time"

	builderapi "github.com/attestantio/go-builder-client/api"
	eth2api "github.com/attestantio/go-eth2-client/api"

	"github.com/ssvlabs/ssv/mev/builderendpoint/unblinder"
)

type fakeProvider struct {
	address string
	delay   time.Duration
	resp    *eth2api.VersionedSignedProposal
	err     error
}

func (p fakeProvider) Address() string { return p.address }

func (p fakeProvider) UnblindProposal(ctx context.Context, _ *builderapi.UnblindProposalOpts) (*builderapi.Response[*eth2api.VersionedSignedProposal], error) {
	if p.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(p.delay):
		}
	}
	if p.err != nil {
		return nil, p.err
	}
	return &builderapi.Response[*eth2api.VersionedSignedProposal]{Data: p.resp}, nil
}

func TestFanoutUnblinder_FirstSuccessWins(t *testing.T) {
	t.Parallel()

	u := &unblinder.FanoutUnblinder{
		Providers: []unblinder.UnblindProvider{
			fakeProvider{address: "a", delay: 10 * time.Millisecond, err: errors.New("boom")},
			fakeProvider{address: "b", delay: 20 * time.Millisecond, resp: &eth2api.VersionedSignedProposal{}},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)

	got, err := u.UnblindBlock(ctx, &eth2api.VersionedSignedBlindedBeaconBlock{})
	if err != nil {
		t.Fatalf("unblind: %v", err)
	}
	if got == nil {
		t.Fatalf("expected unblinded proposal")
	}
}

func TestFanoutUnblinder_AllFailReturnsNil(t *testing.T) {
	t.Parallel()

	u := &unblinder.FanoutUnblinder{
		Providers: []unblinder.UnblindProvider{
			fakeProvider{address: "a", err: errors.New("boom")},
			fakeProvider{address: "b", err: errors.New("boom")},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)

	got, err := u.UnblindBlock(ctx, &eth2api.VersionedSignedBlindedBeaconBlock{})
	if err != nil {
		t.Fatalf("unblind: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil unblinded proposal")
	}
}
