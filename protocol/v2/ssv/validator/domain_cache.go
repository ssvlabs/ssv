package validator

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

type DomainCache struct {
	beaconNode beacon.BeaconNode
	cache      *ttlcache.Cache[domainCacheKey, phase0.Domain]
}

type domainCacheKey struct {
	Epoch      phase0.Epoch
	DomainType phase0.DomainType
}

// NewDomainCache must be Start()-ed the same way as ttlcache.
func NewDomainCache(beaconNode beacon.BeaconNode, ttl time.Duration) *DomainCache {
	return &DomainCache{
		beaconNode: beaconNode,
		cache: ttlcache.New(
			ttlcache.WithTTL[domainCacheKey, phase0.Domain](ttl),
		),
	}
}

func (dc *DomainCache) Start() {
	dc.cache.Start()
}

func (dc *DomainCache) Get(epoch phase0.Epoch, domainType phase0.DomainType) (phase0.Domain, error) {
	key := domainCacheKey{Epoch: epoch, DomainType: domainType}
	item := dc.cache.Get(key)
	if item != nil {
		return item.Value(), nil
	}

	domain, err := dc.beaconNode.DomainData(epoch, domainType)
	if err != nil {
		return phase0.Domain{}, err
	}

	dc.cache.Set(key, domain, ttlcache.DefaultTTL)
	return domain, nil
}
