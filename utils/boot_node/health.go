package bootnode

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/p2p/enode"
)

const (
	// bootReadStaleGrace is how long the socket may go unread before /healthz
	// fails. A boot node is queried continuously, so a wedge stops reads well
	// inside this.
	bootReadStaleGrace = 3 * time.Minute

	// bootEmptyTableGrace tolerates cold start, when the table is briefly empty
	// before the boot node discovers peers.
	bootEmptyTableGrace = 10 * time.Minute
)

// nodeLister is the slice of discovery.Listener the health check needs.
type nodeLister interface {
	AllNodes() []*enode.Node
}

// socketDrainState reports socket-read staleness; satisfied by *discovery.TimedConn.
type socketDrainState interface {
	StaleFor(time.Duration) bool
}

// bootNodeHealth backs /healthz. A boot node's whole job is discovery, so it's
// broken if its socket has wedged or its routing table has been empty past cold
// start; either fails /healthz closed so a liveness probe restarts the pod.
type bootNodeHealth struct {
	lister          nodeLister
	socket          socketDrainState
	readStaleGrace  time.Duration
	emptyTableGrace time.Duration
	now             func() time.Time
	lastNonEmpty    atomic.Int64 // unix nanos of the last observation with a non-empty table
}

func newBootNodeHealth(lister nodeLister, socket socketDrainState) *bootNodeHealth {
	h := &bootNodeHealth{
		lister:          lister,
		socket:          socket,
		readStaleGrace:  bootReadStaleGrace,
		emptyTableGrace: bootEmptyTableGrace,
		now:             time.Now,
	}
	h.lastNonEmpty.Store(h.now().UnixNano())
	return h
}

// check returns a reason when discovery looks broken, or nil when healthy.
//
// The socket-staleness check only applies while the routing table is populated:
// discv5 revalidates those peers and should be reading their responses, so a
// stale socket then means the read loop has wedged. An empty table generates no
// such traffic, so staleness there is expected (cold start, or a quiet node with
// no peers) and is judged only by the empty-table grace, which tolerates cold
// start. lastNonEmpty is seeded at startup, so it catches both "never populated"
// and "populated then emptied".
//
// One case slips past the fast path: a wedge present from boot. On restart discv5
// loads seed nodes from the persistent enode DB straight into the table, so
// AllNodes() can be >0 before the socket has been read once — and StaleFor stays
// disarmed until that first read. Those seeds then fail revalidation and age out,
// the table empties, and the empty-table grace trips instead. A runtime wedge
// (socket drained, then stopped) is unaffected: a prior read has already armed
// StaleFor.
func (h *bootNodeHealth) check() error {
	now := h.now()
	if len(h.lister.AllNodes()) > 0 {
		h.lastNonEmpty.Store(now.UnixNano())
		if h.socket.StaleFor(h.readStaleGrace) {
			return fmt.Errorf("discv5 socket not drained for >%s while routing table is populated", h.readStaleGrace)
		}
		return nil
	}
	if emptyFor := now.Sub(time.Unix(0, h.lastNonEmpty.Load())); emptyFor > h.emptyTableGrace {
		return fmt.Errorf("discv5 routing table empty for >%s", h.emptyTableGrace)
	}
	return nil
}

func (h *bootNodeHealth) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if err := h.check(); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}
}
