package validator

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
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

type BeaconVoteCacheKey struct {
	root   phase0.Root
	height specqbft.Height
}

// BeaconVoteCache is a wrapper around ttlcache.Cache[BeaconVoteCacheKey, struct{}]
// it is needed to avoid passing composite key (BeaconVoteCacheKey) down to the non-committee validator
type BeaconVoteCache struct {
	*ttlcache.Cache[BeaconVoteCacheKey, struct{}]
}

func NewBeaconVoteCacheWrapper(cache *ttlcache.Cache[BeaconVoteCacheKey, struct{}]) *BeaconVoteCache {
	return &BeaconVoteCache{Cache: cache}
}

func (c *BeaconVoteCache) Set(root phase0.Root, height specqbft.Height) {
	c.Cache.Set(BeaconVoteCacheKey{root: root, height: height}, struct{}{}, ttlcache.DefaultTTL)
}

func (c *BeaconVoteCache) Has(root phase0.Root, height specqbft.Height) bool {
	return c.Cache.Has(BeaconVoteCacheKey{root: root, height: height})
}
