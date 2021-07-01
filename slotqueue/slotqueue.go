package slotqueue

import (
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/pubsub"
	"time"

	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/patrickmn/go-cache"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/shared/slotutil"
)

// Queue represents the behavior of the slot queue
type Queue interface {
	// Next returns the next slot with its duties at its time
	RegisterToNext(pubKey []byte) (pubsub.SubjectChannel, error)

	// Schedule schedules execution of the given slot and puts it into the queue
	Schedule(pubKey []byte, slot uint64, duty *ethpb.DutiesResponse_Duty) error

	// listenToTicker and notify for all the observers when tick triggers
	listenToTicker()
}

// queue implements Queue
type queue struct {
	data           *cache.Cache
	ticker         *slotutil.SlotTicker
	tickerSubjects map[string]pubsub.Subject
}

type SlotEvent struct {
	Slot uint64
	Duty *ethpb.DutiesResponse_Duty
	Ok   bool
}

// New is the constructor of queue
func New(network core.Network) Queue {
	genesisTime := time.Unix(int64(network.MinGenesisTime()), 0)
	slotTicker := slotutil.GetSlotTicker(genesisTime, uint64(network.SlotDurationSec().Seconds()))
	queue := &queue{
		data:           cache.New(time.Minute*30, time.Minute*31),
		ticker:         slotTicker,
		tickerSubjects: make(map[string]pubsub.Subject),
	}
	go queue.listenToTicker()
	return queue
}

func (q *queue) listenToTicker() {
	for currentSlot := range q.ticker.C() {
		for pubkey, pub := range q.tickerSubjects {
			key := q.getKey(pubkey, currentSlot)
			dataRaw, ok := q.data.Get(key)
			if !ok {
				continue
			}

			duty, ok := dataRaw.(*ethpb.DutiesResponse_Duty)
			if !ok {
				continue
			}

			pub.Notify(SlotEvent{
				Slot: currentSlot,
				Duty: duty,
				Ok:   true,
			})
		}
	}
	for _, pub := range q.tickerSubjects {
		pub.Notify(SlotEvent{
			Slot: 0,
			Duty: nil,
			Ok:   false,
		})
	}
}

// RegisterToNext check if subject exist if not create new one. Register to the subject and return the subject channel
func (q *queue) RegisterToNext(pubKey []byte) (pubsub.SubjectChannel, error) {
	if pub, ok := q.tickerSubjects[hex.EncodeToString(pubKey)]; ok {
		return pub.Register(hex.EncodeToString(pubKey))
	}
	q.tickerSubjects[hex.EncodeToString(pubKey)] = pubsub.NewSubject()
	return q.tickerSubjects[hex.EncodeToString(pubKey)].Register(hex.EncodeToString(pubKey))
}

// Schedule schedules execution of the given slot and puts it into the queue
func (q *queue) Schedule(pubKey []byte, slot uint64, duty *ethpb.DutiesResponse_Duty) error {
	q.data.SetDefault(q.getKey(hex.EncodeToString(pubKey), slot), duty)
	return nil
}

func (q *queue) getKey(pubKey string, slot uint64) string {
	return fmt.Sprintf("%d_%s", slot, pubKey)
}
