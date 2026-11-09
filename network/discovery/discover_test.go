package discovery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log/fields"
)

// newDiscoverTestService returns a service wired as NewDiscV5Service wires it,
// logging to logger and discovering through listener, but without a real
// discv5 listener: these tests don't need the network, and go-ethereum's table
// revalidation has a data race the race detector can report on a slow runner.
func newDiscoverTestService(t *testing.T, logger *zap.Logger, listener Listener) *DiscV5Service {
	t.Helper()
	dvs := newDiscV5Service(t.Context(), logger, testingDiscoveryOptions(t, testNetConfig.SSV))
	t.Cleanup(dvs.cancel)
	dvs.dv5Listener = listener
	return dvs
}

func TestDiscover_HandlerPanic(t *testing.T) {
	core, logs := observer.New(zapcore.ErrorLevel)
	nodes := NewTestingNodes(t, 2)
	dvs := newDiscoverTestService(t, zap.New(core), NewMockListener(NewLocalNode(t), nodes))

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	var handled []*enode.Node
	dvs.discover(ctx, func(e PeerEvent) {
		if e.Node == nodes[0] {
			panic(errors.New("handler panic"))
		}
		handled = append(handled, e.Node)
		cancel()
	}, time.Millisecond)

	require.Equal(t, []*enode.Node{nodes[1]}, handled, "the node after the panicking one must still be handled")

	entries := logs.FilterMessage("recovered from panic while handling discovered node").All()
	require.Len(t, entries, 1)
	logged := entries[0].ContextMap()
	require.Equal(t, nodes[0].String(), logged[fields.FieldTargetNodeENR])
	require.Contains(t, logged["panic"], "handler panic")
	require.NotEmpty(t, logged["stack"])
}

func TestDiscover_FilterPanic(t *testing.T) {
	core, logs := observer.New(zapcore.ErrorLevel)
	nodes := NewTestingNodes(t, 2)
	dvs := newDiscoverTestService(t, zap.New(core), NewMockListener(NewLocalNode(t), nodes))

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	panickingFilter := func(n *enode.Node) bool {
		if n == nodes[0] {
			panic(errors.New("filter panic"))
		}
		return true
	}

	var handled []*enode.Node
	dvs.discover(ctx, func(e PeerEvent) {
		handled = append(handled, e.Node)
		cancel()
	}, time.Millisecond, panickingFilter)

	require.Equal(t, []*enode.Node{nodes[1]}, handled, "a node whose filter panicked must be dropped and the next one handled")

	entries := logs.FilterMessage("recovered from panic while filtering discovered node").All()
	require.Len(t, entries, 1)
	logged := entries[0].ContextMap()
	require.Equal(t, nodes[0].String(), logged[fields.FieldTargetNodeENR])
	require.Contains(t, logged["panic"], "filter panic")
}

func TestReportRecoveredPanic_RateLimited(t *testing.T) {
	core, logs := observer.New(zapcore.ErrorLevel)
	dvs := newDiscoverTestService(t, zap.New(core), nil)
	node := NewTestingNode(t)
	skippedBefore := peersSkipped(t, skipReasonFilterPanic)

	for range 3 {
		dvs.reportRecoveredPanic(t.Context(), skipReasonFilterPanic, "recovered", node, "boom")
	}
	require.Len(t, logs.All(), 1, "panics within panicLogInterval of a logged one with the same skip reason must not be logged")

	// Each skip reason has its own limit and count, so a handler panic is logged
	// right after a filter panic and doesn't count the filter panics skipped.
	dvs.reportRecoveredPanic(t.Context(), skipReasonHandlerPanic, "recovered", node, "boom")
	entries := logs.All()
	require.Len(t, entries, 2, "a panic with another skip reason must be logged")
	require.EqualValues(t, 0, entries[1].ContextMap()["panics_not_logged"])

	// Once panicLogInterval has passed, the next panic is logged with the count of those skipped.
	filterLog := dvs.panicLogs[skipReasonFilterPanic]
	filterLog.lastLog = filterLog.lastLog.Add(-panicLogInterval)
	dvs.reportRecoveredPanic(t.Context(), skipReasonFilterPanic, "recovered", node, "boom")
	entries = logs.All()
	require.Len(t, entries, 3)
	require.EqualValues(t, 2, entries[2].ContextMap()["panics_not_logged"])

	// That log reset the count, so the next interval's log has nothing skipped to report.
	filterLog.lastLog = filterLog.lastLog.Add(-panicLogInterval)
	dvs.reportRecoveredPanic(t.Context(), skipReasonFilterPanic, "recovered", node, "boom")
	entries = logs.All()
	require.Len(t, entries, 4)
	require.EqualValues(t, 0, entries[3].ContextMap()["panics_not_logged"])

	// Every panic counts as a skipped peer, logged or not.
	require.EqualValues(t, 5, peersSkipped(t, skipReasonFilterPanic)-skippedBefore)
}

func TestReportRecoveredPanic_StructLiteralService(t *testing.T) {
	core, logs := observer.New(zapcore.ErrorLevel)
	// Several tests build a DiscV5Service as a struct literal, with no rate-limit
	// state, and a panic in reportRecoveredPanic would escape its recover.
	dvs := &DiscV5Service{logger: zap.New(core)}

	require.NotPanics(t, func() {
		dvs.reportRecoveredPanic(t.Context(), skipReasonFilterPanic, "recovered", NewTestingNode(t), "boom")
	})
	require.Len(t, logs.All(), 1)
}

// peersSkipped returns the package-global skipped-peers counter's total for
// reason, read via pkgTestMetricReader; 0 if no peer was skipped for it yet.
func peersSkipped(t *testing.T, reason skipReason) int64 {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, pkgTestMetricReader.Collect(t.Context(), &rm))

	reasonKey := peerSkipReasonAttribute(reason).Key
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != observability.InstrumentName(observabilityNamespace, "peers.skipped") {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok, "expected Sum[int64], got %T", m.Data)
			for _, dp := range sum.DataPoints {
				if v, ok := dp.Attributes.Value(reasonKey); ok && v.AsString() == string(reason) {
					return dp.Value
				}
			}
		}
	}
	return 0
}
