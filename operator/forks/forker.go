package forks

import (
	"github.com/bloxapp/eth2-key-manager/core"
	"go.uber.org/zap"
	"sync"
	"sync/atomic"
)

// OnFork handles fork event
type OnFork func(slot uint64)

// state for fork event
const (
	stateBefore uint64 = 0
	stateAfter  uint64 = 1
)

// Config for Forker struct setup
type Config struct {
	Network    string
	Logger     *zap.Logger
	BeforeFork Fork
	PostFork   Fork
	ForkSlot   uint64
}

// Forker managing fork events
type Forker struct {
	logger   *zap.Logger
	network  core.Network
	state    uint64
	forkSlot uint64
	handlers []OnFork

	postFork    Fork
	currentFork Fork
	forkLock    sync.RWMutex
}

// NewForker return pointer to new forker struct
func NewForker(cfg Config) *Forker {
	return &Forker{
		logger:      cfg.Logger.With(zap.String("who", "forker")),
		network:     core.NetworkFromString(cfg.Network),
		forkSlot:    cfg.ForkSlot,
		state:       stateBefore,
		currentFork: cfg.BeforeFork,
		postFork:    cfg.PostFork,
		forkLock:    sync.RWMutex{},
	}
}

// Start forker essential processes
func (f *Forker) Start() {
	//	 update slot tick with current slot
	f.SlotTick(f.currentSlot())
}

// AddHandler register new handler to the handlers queue
func (f *Forker) AddHandler(handler OnFork) {
	f.handlers = append(f.handlers, handler)
}

// SlotTick get the current slot and start fork process if not forked yet and fork slot is match.
// notify the registered handlers
// and set the new current fork
func (f *Forker) SlotTick(slot uint64) {
	if slot >= f.forkSlot && !f.IsForked() { // TODo check if can do this code with atomic func
		f.logger.Debug("on fork!", zap.Uint64("slot", slot))
		f.forkLock.Lock()
		f.currentFork = f.postFork
		f.forkLock.Unlock()

		f.logger.Debug("calling handlers", zap.Int("size", len(f.handlers)))
		for _, handler := range f.handlers {
			handler(slot)
		}
		atomic.StoreUint64(&f.state, stateAfter)
	}
}

// IsForked returns true if fork already happened
func (f *Forker) IsForked() bool {
	return atomic.LoadUint64(&f.state) == stateAfter
}

// GetCurrentFork returns the current fork
func (f *Forker) GetCurrentFork() Fork {
	f.forkLock.RLock()
	defer f.forkLock.RUnlock()
	return f.currentFork
}

// currentSlot return the network current slot
func (f *Forker) currentSlot() uint64 {
	return uint64(f.network.EstimatedCurrentSlot())
}
