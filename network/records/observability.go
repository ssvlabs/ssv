package records

import (
	"context"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/metrics"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/network/records"
	observabilityNamespace = "ssv.p2p.records"

	rejectLogInterval = time.Minute
)

var (
	meter = otel.Meter(observabilityName)

	recordsRejectedCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "rejected"),
			metric.WithUnit("{record}"),
			metric.WithDescription("total number of ENRs rejected by discv5 record verification for an invalid domain type entry")))

	// rejectLogMu guards lastRejectLog and rejectsNotLogged, which rate-limit recordRejected's log.
	rejectLogMu      sync.Mutex
	lastRejectLog    time.Time
	rejectsNotLogged uint64
)

func rejectedKeyAttribute(key ENRKey) attribute.KeyValue {
	return attribute.String("ssv.p2p.records.enr_key", string(key))
}

// recordRejected counts a record of node id that validRecordScheme rejected for
// key, and logs it at debug at most once per rejectLogInterval. go-ethereum
// drops such records inside discv5, before discovery's checkPeer could count
// them. The counter is the signal to alert on; the operator can't act on the log.
func recordRejected(id enode.ID, key ENRKey, err error) {
	recordsRejectedCounter.Add(context.Background(), 1, metric.WithAttributes(rejectedKeyAttribute(key)))

	rejectLogMu.Lock()
	if time.Since(lastRejectLog) < rejectLogInterval {
		rejectsNotLogged++
		rejectLogMu.Unlock()
		return
	}
	lastRejectLog = time.Now()
	notLogged := rejectsNotLogged
	rejectsNotLogged = 0
	rejectLogMu.Unlock()

	// records has no logger of its own; zap.L() is the node's global logger.
	zap.L().Debug("rejected ENR with invalid domain type entry",
		zap.Stringer("node_id", id),
		zap.String("enr_key", string(key)),
		zap.Error(err),
		zap.Uint64("rejects_not_logged", notLogged),
	)
}
