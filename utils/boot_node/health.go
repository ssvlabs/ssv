package bootnode

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"go.uber.org/zap"
)

const (
	// bootReadStaleGrace is how long the socket may go unread before /healthz
	// fails. A boot node is queried continuously, so a wedge stops reads well
	// inside this.
	bootReadStaleGrace = 3 * time.Minute

	// bootEmptyTableGrace tolerates cold start, when the table is briefly empty
	// before the boot node discovers peers.
	bootEmptyTableGrace = 10 * time.Minute

	// bootHealthSamplePeriod is how often the routing table is sampled; health
	// verdicts read the samples (see sample).
	bootHealthSamplePeriod = time.Second
)

// nodeLister is the slice of discovery.Listener the health check needs.
type nodeLister interface {
	AllNodes() []*enode.Node
}

// socketDrainState reports how long the discv5 socket has gone unread and
// whether it has ever been read; satisfied by *discovery.TimedConn.
type socketDrainState interface {
	ReadStaleness() (time.Duration, bool)
}

// bootNodeHealth backs /healthz. A boot node's whole job is discovery, so it is
// broken when its socket has wedged or its routing table has been empty past
// cold start; both fail /healthz closed so a liveness probe restarts the pod.
// The one deliberate exception — an empty table while the socket is demonstrably
// drained — is logged instead: see check.
type bootNodeHealth struct {
	logger          *zap.Logger
	lister          nodeLister
	socket          socketDrainState
	readStaleGrace  time.Duration
	emptyTableGrace time.Duration
	samplePeriod    time.Duration
	now             func() time.Time

	// Table state as of the last sample; written by sample, read by check.
	tablePopulated atomic.Bool
	lastNonEmpty   atomic.Int64 // unix nanos of the last sample with a non-empty table
}

func newBootNodeHealth(logger *zap.Logger, lister nodeLister, socket socketDrainState) *bootNodeHealth {
	h := &bootNodeHealth{
		logger:          logger,
		lister:          lister,
		socket:          socket,
		readStaleGrace:  bootReadStaleGrace,
		emptyTableGrace: bootEmptyTableGrace,
		samplePeriod:    bootHealthSamplePeriod,
		now:             time.Now,
	}
	h.lastNonEmpty.Store(h.now().UnixNano())
	return h
}

// start launches the table sampler; it stops when ctx is canceled.
func (h *bootNodeHealth) start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(h.samplePeriod)
		defer ticker.Stop()
		h.sample()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.sample()
			}
		}
	}()
}

// sample records whether the routing table is currently populated. It is the
// only place the table is consulted (AllNodes takes the discv5 table mutex and
// allocates), keeping /healthz requests cheap and the empty-table clock
// probe-independent: the clock advances with the table, not with whatever probe
// cadence the endpoint happens to see.
func (h *bootNodeHealth) sample() {
	populated := len(h.lister.AllNodes()) > 0
	h.tablePopulated.Store(populated)
	if populated {
		h.lastNonEmpty.Store(h.now().UnixNano())
	}
}

// check returns a reason when discovery looks broken, or nil when healthy.
//
// Socket staleness only counts while the routing table is populated: discv5
// revalidates those peers and should be reading their responses, so a stale
// socket then means the read loop has wedged. An empty table generates no such
// traffic, so staleness there is expected (cold start, or a quiet node).
//
// An empty table past the grace is judged by the socket:
//   - undrained (never read, or read then gone stale): fail closed — nothing
//     arrives or the read path is dead, and a restart may recover it. This is
//     the definitional boot-node failure (#2979).
//   - actively drained: healthy, with an error log — packets arrive and are
//     read, yet nothing completes a handshake into the table. That is a
//     config/compatibility mismatch (e.g. DiscoveryProtocolID) a restart cannot
//     fix; failing would crash-loop the pod and take /p2p down with it.
//
// Accepted blind spot: a table populated from persistent-DB seeds while the
// socket has never been read. Normally revalidation ages such seeds out (its
// pings time out, failed peers are dropped), emptying the table so the rules
// above take over — but a dispatch loop wedged before the first socket read
// stalls revalidation with the seeds in place, and this check stays green.
// That takes a from-boot dispatch wedge with zero packets ever read (a single
// read arms the staleness check); no known cause since #2980.
func (h *bootNodeHealth) check() error {
	if h.tablePopulated.Load() {
		if age, everRead := h.socket.ReadStaleness(); everRead && age > h.readStaleGrace {
			return fmt.Errorf("discv5 socket not drained for >%s while routing table is populated", h.readStaleGrace)
		}
		return nil
	}
	emptyFor := h.now().Sub(time.Unix(0, h.lastNonEmpty.Load()))
	if emptyFor <= h.emptyTableGrace {
		return nil
	}
	if age, everRead := h.socket.ReadStaleness(); everRead && age <= h.readStaleGrace {
		h.logger.Error("discv5 routing table empty past grace while socket is drained: a restart cannot fix this, check DiscoveryProtocolID/network config against the fleet",
			zap.Duration("empty_for", emptyFor), zap.Duration("read_age", age))
		return nil
	}
	return fmt.Errorf("discv5 routing table empty for >%s and socket undrained", h.emptyTableGrace)
}

// handler serves /healthz. The endpoint is reachable on the ENR-advertised TCP
// port, so it stays cheap and opaque: sampled state only, and a fixed body —
// the reason is logged, not disclosed.
func (h *bootNodeHealth) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if err := h.check(); err != nil {
			h.logger.Warn("healthz check failed", zap.Error(err))
			http.Error(w, "unhealthy", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}
}
