package goclient

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"
)

// CommitteesForEpoch fetches all committees for an epoch and caches them.
func (gc *GoClient) CommitteesForEpoch(ctx context.Context, epoch phase0.Epoch) ([]*eth2apiv1.BeaconCommittee, error) {
	if gc.committeesCache != nil {
		if cached := gc.committeesCache.Get(epoch); cached != nil {
			return cached.Value(), nil
		}
	}

	reqStart := time.Now()
	resp, err := gc.multiClient.BeaconCommittees(ctx, &api.BeaconCommitteesOpts{
		State:  "head",
		Epoch:  &epoch,
		Common: api.CommonOpts{Timeout: gc.commonTimeout},
	})
	recordRequest(ctx, gc.log, "BeaconCommittees", gc.multiClient, http.MethodGet, true, time.Since(reqStart), err)
	if err != nil {
		return nil, errMultiClient(fmt.Errorf("fetch beacon committees: %w", err), "BeaconCommittees")
	}
	if resp == nil || resp.Data == nil {
		return nil, errMultiClient(fmt.Errorf("beacon committees response is nil"), "BeaconCommittees")
	}
	if gc.committeesCache != nil {
		gc.committeesCache.Set(epoch, resp.Data, ttlcache.DefaultTTL)
	}
	return resp.Data, nil
}

// CommitteesForSlot returns committees for a slot by filtering the epoch response.
func (gc *GoClient) CommitteesForSlot(ctx context.Context, slot phase0.Slot) ([]*eth2apiv1.BeaconCommittee, error) {
	cfg := gc.getBeaconConfig()
	if cfg == nil {
		return nil, fmt.Errorf("no beacon config set")
	}
	epoch := cfg.EstimatedEpochAtSlot(slot)
	all, err := gc.CommitteesForEpoch(ctx, epoch)
	if err != nil {
		return nil, err
	}
	out := make([]*eth2apiv1.BeaconCommittee, 0, 8)
	for _, c := range all {
		if c != nil && c.Slot == slot {
			out = append(out, c)
		}
	}
	return out, nil
}
