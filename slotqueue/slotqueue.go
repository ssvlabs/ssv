package slotqueue

import (
	"fmt"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/herumi/bls-eth-go-binary/bls"

	"time"

	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/patrickmn/go-cache"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/shared/slotutil"
)

// Queue represents the behavior of the slot queue
type Queue interface {
	// Next returns the next slot with its duties at its time
	Next(pubKey []byte) (uint64, *ethpb.DutiesResponse_Duty, bool, error)

	// Schedule schedules execution of the given slot and puts it into the queue
	Schedule(pubKey []byte, slot uint64, duty *ethpb.DutiesResponse_Duty) error
}

// queue implements Queue
type queue struct {
	data   *cache.Cache
	ticker *slotutil.SlotTicker
}

// Duty struct  TODO: need to remove
type Duty struct {
	NodeID uint64
	Duty   *ethpb.DutiesResponse_Duty
	// ValidatorPK is the validator's public key
	ValidatorPK *bls.PublicKey
	// ShareSK is this node's share secret key
	ShareSK   *bls.SecretKey
	Committee map[uint64]*proto.Node
}

// New is the constructor of queue
func New(network core.Network) Queue {
	genesisTime := time.Unix(int64(network.MinGenesisTime()), 0)
	slotTicker := slotutil.GetSlotTicker(genesisTime, uint64(network.SlotDurationSec().Seconds()))
	return &queue{
		data:   cache.New(time.Minute*30, time.Minute*31),
		ticker: slotTicker,
	}
}

// Next returns the next slot with its duties at its time
func (q *queue) Next(pubKey []byte) (uint64, *ethpb.DutiesResponse_Duty, bool, error) {
	for currentSlot := range q.ticker.C() {
		key := q.getKey(pubKey, currentSlot)
		dataRaw, ok := q.data.Get(key)
		if !ok {
			continue
		}

		duty, ok := dataRaw.(*ethpb.DutiesResponse_Duty)
		if !ok {
			continue
		}

		return currentSlot, duty, true, nil
	}

	return 0, nil, false, nil
}

// Schedule schedules execution of the given slot and puts it into the queue
func (q *queue) Schedule(pubKey []byte, slot uint64, duty *ethpb.DutiesResponse_Duty) error {
	q.data.SetDefault(q.getKey(pubKey, slot), duty)
	return nil
}

func (q *queue) getKey(pubKey []byte, slot uint64) string {
	return fmt.Sprintf("%d_%#v", slot, pubKey)
}
