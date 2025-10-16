package duties

import (
	"context"
	"errors"
	"sync"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	goclient "github.com/ssvlabs/ssv/beacon/goclient"
	"github.com/ssvlabs/ssv/networkconfig"
	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

type validatorPubkeyProvider interface {
	ValidatorPubkey(index phase0.ValidatorIndex) (spectypes.ValidatorPK, bool)
}

// prefetchingBeacon adapts the scheduler BeaconNode interface, prefetching
// epoch/period data and serving handler calls from cache. It instruments
// network request duration and processing time.
type prefetchingBeacon struct {
	log    *zap.Logger
	inner  beaconprotocol.BeaconNode
	cfg    *networkconfig.Beacon
	vstore validatorPubkeyProvider

	committees *ttlcache.Cache[phase0.Epoch, []*eth2apiv1.BeaconCommittee]

	muAtt sync.RWMutex
	// epoch -> index -> duty
	attester map[phase0.Epoch]map[phase0.ValidatorIndex]*eth2apiv1.AttesterDuty

	muProp   sync.RWMutex
	proposer map[phase0.Epoch]map[phase0.ValidatorIndex]*eth2apiv1.ProposerDuty

	muSync sync.RWMutex
	// period -> index -> duty
	sync map[uint64]map[phase0.ValidatorIndex]*eth2apiv1.SyncCommitteeDuty
}

// ErrCommitteesUnsupported is returned when the underlying beacon client does not
// support fetching committees for an epoch (CommitteesForEpoch). This indicates
// that the prefetch optimization is unavailable for the configured client type.
var ErrCommitteesUnsupported = errors.New("beacon client does not support committees prefetch")

func NewPrefetchingBeacon(log *zap.Logger, inner beaconprotocol.BeaconNode, cfg *networkconfig.Beacon, vstore validatorPubkeyProvider) *prefetchingBeacon {
	c := ttlcache.New(ttlcache.WithTTL[phase0.Epoch, []*eth2apiv1.BeaconCommittee](2 * cfg.EpochDuration()))
	go c.Start()
	return &prefetchingBeacon{
		log:        log.Named("beacon-prefetch"),
		inner:      inner,
		cfg:        cfg,
		vstore:     vstore,
		committees: c,
		attester:   make(map[phase0.Epoch]map[phase0.ValidatorIndex]*eth2apiv1.AttesterDuty),
		proposer:   make(map[phase0.Epoch]map[phase0.ValidatorIndex]*eth2apiv1.ProposerDuty),
		sync:       make(map[uint64]map[phase0.ValidatorIndex]*eth2apiv1.SyncCommitteeDuty),
	}
}

// ===== Helpers =====

// committeesForEpoch loads committees from cache or underlying client.
func (p *prefetchingBeacon) committeesForEpoch(ctx context.Context, epoch phase0.Epoch) ([]*eth2apiv1.BeaconCommittee, error) {
	if item := p.committees.Get(epoch); item != nil {
		return item.Value(), nil
	}
	// Attempt type assertion to GoClient for CommitteesForEpoch.
	if gc, ok := p.inner.(interface {
		CommitteesForEpoch(context.Context, phase0.Epoch) ([]*eth2apiv1.BeaconCommittee, error)
	}); ok {
		start := time.Now()
		comm, err := gc.CommitteesForEpoch(ctx, epoch)
		p.log.Debug("fetch CommitteesForEpoch", zap.Uint64("epoch", uint64(epoch)), zap.Duration("took", time.Since(start)), zap.Error(err))
		if err != nil {
			return nil, err
		}
		_ = p.committees.Set(epoch, comm, ttlcache.DefaultTTL)
		return comm, nil
	}
	// Fallback: underlying client may be goclient.GoClient explicitly.
	if gc2, ok := p.inner.(*goclient.GoClient); ok {
		start := time.Now()
		comm, err := gc2.CommitteesForEpoch(ctx, epoch)
		p.log.Debug("fetch CommitteesForEpoch(GoClient)", zap.Uint64("epoch", uint64(epoch)), zap.Duration("took", time.Since(start)), zap.Error(err))
		if err != nil {
			return nil, err
		}
		_ = p.committees.Set(epoch, comm, ttlcache.DefaultTTL)
		return comm, nil
	}
	return nil, ErrCommitteesUnsupported
}

func (p *prefetchingBeacon) ensureAttesterEpoch(ctx context.Context, epoch phase0.Epoch) error {
	p.muAtt.RLock()
	_, ok := p.attester[epoch]
	p.muAtt.RUnlock()
	if ok {
		return nil
	}
	comm, err := p.committeesForEpoch(ctx, epoch)
	if err != nil {
		return err
	}
	start := time.Now()
	dutyMap := make(map[phase0.ValidatorIndex]*eth2apiv1.AttesterDuty, 1_000)
	perSlotCount := make(map[phase0.Slot]uint64, 32)
	for _, c := range comm {
		if c == nil {
			continue
		}
		perSlotCount[c.Slot]++
	}
	// Build index→duty from committees with correct CommitteesAtSlot.
	for _, c := range comm {
		if c == nil {
			continue
		}
		committeesAtSlot := perSlotCount[c.Slot]
		for pos, idx := range c.Validators {
			duty := &eth2apiv1.AttesterDuty{
				Slot:                    c.Slot,
				ValidatorIndex:          idx,
				CommitteeIndex:          c.Index,
				CommitteeLength:         uint64(len(c.Validators)),
				CommitteesAtSlot:        committeesAtSlot,
				ValidatorCommitteeIndex: uint64(pos), // #nosec G115
			}
			if pk, ok := p.vstore.ValidatorPubkey(idx); ok {
				var bls phase0.BLSPubKey
				copy(bls[:], pk[:])
				duty.PubKey = bls
			}
			dutyMap[idx] = duty
		}
	}
	p.muAtt.Lock()
	p.attester[epoch] = dutyMap
	p.muAtt.Unlock()
	p.log.Debug("build attester cache", zap.Uint64("epoch", uint64(epoch)), zap.Int("indices", len(dutyMap)), zap.Duration("took", time.Since(start)))
	return nil
}

func (p *prefetchingBeacon) ensureProposerEpoch(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) error {
	p.muProp.RLock()
	m, ok := p.proposer[epoch]
	p.muProp.RUnlock()
	if ok && len(m) > 0 {
		return nil
	}
	// Batch the request indexes to avoid oversized calls.
	const batch = 2048
	collected := make(map[phase0.ValidatorIndex]*eth2apiv1.ProposerDuty)
	for i := 0; i < len(indices); i += batch {
		end := i + batch
		if end > len(indices) {
			end = len(indices)
		}
		part := indices[i:end]
		start := time.Now()
		duties, err := p.inner.ProposerDuties(ctx, epoch, part)
		p.log.Debug("fetch ProposerDuties", zap.Uint64("epoch", uint64(epoch)), zap.Int("req_indices", len(part)), zap.Int("duties", len(duties)), zap.Duration("took", time.Since(start)), zap.Error(err))
		if err != nil {
			return err
		}
		for _, d := range duties {
			collected[d.ValidatorIndex] = d
		}
	}
	p.muProp.Lock()
	p.proposer[epoch] = collected
	p.muProp.Unlock()
	return nil
}

func (p *prefetchingBeacon) ensureSyncPeriod(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) error {
	period := p.cfg.EstimatedSyncCommitteePeriodAtEpoch(epoch)
	p.muSync.RLock()
	_, ok := p.sync[period]
	p.muSync.RUnlock()
	if ok {
		return nil
	}
	// Batch fetch missing via underlying call; if client supports a period bulk endpoint we can swap later.
	const batch = 2048
	collected := make(map[phase0.ValidatorIndex]*eth2apiv1.SyncCommitteeDuty)
	for i := 0; i < len(indices); i += batch {
		end := i + batch
		if end > len(indices) {
			end = len(indices)
		}
		part := indices[i:end]
		start := time.Now()
		duties, err := p.inner.SyncCommitteeDuties(ctx, epoch, part)
		p.log.Debug("fetch SyncCommitteeDuties", zap.Uint64("epoch", uint64(epoch)), zap.Int("req_indices", len(part)), zap.Int("duties", len(duties)), zap.Duration("took", time.Since(start)), zap.Error(err))
		if err != nil {
			return err
		}
		for _, d := range duties {
			collected[d.ValidatorIndex] = d
		}
	}
	p.muSync.Lock()
	p.sync[period] = collected
	p.muSync.Unlock()
	return nil
}

// ===== Scheduler BeaconNode subset =====

func (p *prefetchingBeacon) AttesterDuties(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error) {
	if err := p.ensureAttesterEpoch(ctx, epoch); err != nil {
		return nil, err
	}
	p.muAtt.RLock()
	m := p.attester[epoch]
	p.muAtt.RUnlock()
	out := make([]*eth2apiv1.AttesterDuty, 0, len(indices))
	for _, idx := range indices {
		if d, ok := m[idx]; ok {
			out = append(out, d)
		}
	}
	return out, nil
}

func (p *prefetchingBeacon) ProposerDuties(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.ProposerDuty, error) {
	if err := p.ensureProposerEpoch(ctx, epoch, indices); err != nil {
		return nil, err
	}
	p.muProp.RLock()
	m := p.proposer[epoch]
	p.muProp.RUnlock()
	out := make([]*eth2apiv1.ProposerDuty, 0, len(indices))
	for _, idx := range indices {
		if d, ok := m[idx]; ok {
			out = append(out, d)
		}
	}
	return out, nil
}

func (p *prefetchingBeacon) SyncCommitteeDuties(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.SyncCommitteeDuty, error) {
	if err := p.ensureSyncPeriod(ctx, epoch, indices); err != nil {
		return nil, err
	}
	period := p.cfg.EstimatedSyncCommitteePeriodAtEpoch(epoch)
	p.muSync.RLock()
	m := p.sync[period]
	p.muSync.RUnlock()
	out := make([]*eth2apiv1.SyncCommitteeDuty, 0, len(indices))
	for _, idx := range indices {
		if d, ok := m[idx]; ok {
			out = append(out, d)
		}
	}
	return out, nil
}

func (p *prefetchingBeacon) SubmitBeaconCommitteeSubscriptions(ctx context.Context, subscription []*eth2apiv1.BeaconCommitteeSubscription) error {
	// Pass-through
	return p.inner.SubmitBeaconCommitteeSubscriptions(ctx, subscription)
}

func (p *prefetchingBeacon) SubmitSyncCommitteeSubscriptions(ctx context.Context, subscription []*eth2apiv1.SyncCommitteeSubscription) error {
	// Pass-through
	return p.inner.SubmitSyncCommitteeSubscriptions(ctx, subscription)
}

func (p *prefetchingBeacon) SubscribeToHeadEvents(ctx context.Context, subscriberIdentifier string, ch chan<- *eth2apiv1.HeadEvent) error {
	return p.inner.SubscribeToHeadEvents(ctx, subscriberIdentifier, ch)
}

// Ensure conformance to the scheduler BeaconNode subset (defined in scheduler.go).
var _ BeaconNode = (*prefetchingBeacon)(nil)
