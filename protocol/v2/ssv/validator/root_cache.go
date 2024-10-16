package validator

import (
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"
)

type BeaconVoteCache struct {
	cache *ttlcache.Cache[beaconVoteKey, map[phase0.CommitteeIndex]phase0.Root]
}

type beaconVoteKey struct {
	VoteHash phase0.Root
}

// NewBeaconVoteCache initializes the cache with a specified TTL.
func NewBeaconVoteCache(ttl time.Duration) *BeaconVoteCache {
	return &BeaconVoteCache{
		cache: ttlcache.New(
			ttlcache.WithTTL[beaconVoteKey, map[phase0.CommitteeIndex]phase0.Root](ttl),
		),
	}
}

func (bvc *BeaconVoteCache) Start() {
	bvc.cache.Start()
}

// Get checks if the cache contains the signing roots for the given beacon vote.
func (bvc *BeaconVoteCache) Get(voteHash phase0.Root) (map[phase0.CommitteeIndex]phase0.Root, bool) {
	key := beaconVoteKey{VoteHash: voteHash}
	item := bvc.cache.Get(key)
	if item != nil {
		return item.Value(), true
	}
	return nil, false
}

// Set stores the signing roots in the cache for the given beacon vote.
func (bvc *BeaconVoteCache) Set(voteHash phase0.Root, roots map[phase0.CommitteeIndex]phase0.Root) {
	key := beaconVoteKey{VoteHash: voteHash}
	bvc.cache.Set(key, roots, ttlcache.DefaultTTL)
}
