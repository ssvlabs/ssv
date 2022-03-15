package forks

import (
	"github.com/bloxapp/eth2-key-manager/core"
	"sync"
	"sync/atomic"
)

type OnFork func(slot uint64)

const (
	stateBefore  uint64 = 0
	stateForking uint64 = 1
	stateAfter   uint64 = 2
)

type Config struct {
	Network    string
	BeforeFork Fork
	PostFork   Fork
	ForkSlot   uint64
}

type Forker struct {
	network  core.Network
	state    uint64
	forkSlot uint64
	handlers []OnFork

	postFork    Fork
	currentFork Fork
	forkLock    sync.RWMutex
}

func NewForker(cfg Config) *Forker {
	return &Forker{
		network:     core.NetworkFromString(cfg.Network),
		forkSlot:    cfg.ForkSlot,
		state:       stateBefore,
		currentFork: cfg.BeforeFork,
		postFork:    cfg.PostFork,
		forkLock:    sync.RWMutex{},
	}
}

func (f *Forker) Start() {
	//	 update slot tick with current slot
	f.SlotTick(f.currentSlot())
}

func (f *Forker) AddHandler(handler OnFork) {
	f.handlers = append(f.handlers, handler)
}

func (f *Forker) SlotTick(slot uint64) {
	if slot >= f.forkSlot && !f.IsForked() { // TODo check if can do this code with atomic func
		f.forkLock.Lock()
		f.currentFork = f.postFork
		f.forkLock.Unlock()

		for _, handler := range f.handlers {
			handler(slot)
		}
	}
	atomic.StoreUint64(&f.state, stateAfter)
}

func (f *Forker) IsForked() bool {
	return atomic.LoadUint64(&f.state) == stateAfter
}

func (f *Forker) GetCurrentFork() Fork {
	f.forkLock.RLock()
	defer f.forkLock.RUnlock()
	return f.currentFork
}

func (f *Forker) currentSlot() uint64 {
	return uint64(f.network.EstimatedCurrentSlot())
}
