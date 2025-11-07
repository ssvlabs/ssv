package span

import (
	"context"
	"time"

	"github.com/jellydator/ttlcache/v3"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/ssvlabs/ssv/utils/hashmap"
)

// SubtreeDropProcessor drops a marked span and all of its descendants.
// Call MarkDropSubtree(span.SpanContext()) at any time before those spans are exported.
type SubtreeDropProcessor struct {
	inner sdktrace.SpanProcessor

	// childToParent is a per-trace -> child->parent map
	childToParent *ttlcache.Cache[trace.TraceID, *hashmap.Map[trace.SpanID, trace.SpanID]]
	// parentsToDrop is a per-trace -> parent-to-drop
	parentsToDrop *ttlcache.Cache[trace.TraceID, *hashmap.Map[trace.SpanID, struct{}]]
}

func NewSubtreeDropProcessor(inner sdktrace.SpanProcessor) *SubtreeDropProcessor {
	// TTL of 5 minutes should be enough to process all the span-drops we might need to process.
	ttl := 5 * time.Minute

	childToParentCache := ttlcache.New(
		ttlcache.WithTTL[trace.TraceID, *hashmap.Map[trace.SpanID, trace.SpanID]](ttl),
	)
	childToParentCache.Start()

	parentsToDropCache := ttlcache.New(
		ttlcache.WithTTL[trace.TraceID, *hashmap.Map[trace.SpanID, struct{}]](ttl),
	)
	parentsToDropCache.Start()

	return &SubtreeDropProcessor{
		inner:         inner,
		childToParent: childToParentCache,
		parentsToDrop: parentsToDropCache,
	}
}

// MarkDropSubtree marks the given span (and descendants) to be dropped.
func (p *SubtreeDropProcessor) MarkDropSubtree(sc trace.SpanContext) {
	// Perform a defensive sanity-check just in case.
	if !sc.IsValid() {
		return
	}

	dropR, _ := p.parentsToDrop.GetOrSet(sc.TraceID(), hashmap.New[trace.SpanID, struct{}]())
	dropR.Value().Set(sc.SpanID(), struct{}{})
}

func (p *SubtreeDropProcessor) OnStart(ctx context.Context, s sdktrace.ReadWriteSpan) {
	ctp, _ := p.childToParent.GetOrSet(s.SpanContext().TraceID(), hashmap.New[trace.SpanID, trace.SpanID]())
	ctp.Value().Set(s.SpanContext().SpanID(), s.Parent().SpanID())

	p.inner.OnStart(ctx, s)
}

func (p *SubtreeDropProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	if p.shouldDrop(s.SpanContext().TraceID(), s.SpanContext().SpanID()) {
		return // drop this span
	}

	p.inner.OnEnd(s)
}

func (p *SubtreeDropProcessor) shouldDrop(tId trace.TraceID, sId trace.SpanID) bool {
	parentsToDropItem := p.parentsToDrop.Get(tId)
	if parentsToDropItem == nil {
		return false // nothing to drop
	}
	parentsToDrop := parentsToDropItem.Value()

	// Check and exit fast if there is nothing to drop.
	if parentsToDrop.SlowLen() == 0 {
		return false // nothing to drop
	}

	childToParentItem := p.childToParent.Get(tId)
	if childToParentItem == nil {
		return false // nothing to drop
	}
	childToParent := childToParentItem.Value()

	// Walk up the span-chain (child -> parent -> ... -> root) to see if the sId span has a parent that
	// has been dropped, and if so - we need to drop the child as well.
	child := sId
	for {
		if parentsToDrop.Has(child) {
			return true // found the parent that needs to be dropped
		}
		parent, ok := childToParent.Get(child)
		if !ok || !parent.IsValid() {
			return false // traversal is done
		}
		child = parent
	}
}

func (p *SubtreeDropProcessor) Shutdown(ctx context.Context) error {
	p.childToParent.Stop()
	p.parentsToDrop.Stop()

	return p.inner.Shutdown(ctx)
}

func (p *SubtreeDropProcessor) ForceFlush(ctx context.Context) error {
	return p.inner.ForceFlush(ctx)
}
