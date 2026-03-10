package stats

import "testing"

func TestHistogramQuantileUpperBound(t *testing.T) {
	t.Parallel()

	h := NewHistogram([]float64{1, 2, 3})
	h.Record(0.5) // bucket 1
	h.Record(1.5) // bucket 2
	h.Record(2.5) // bucket 3
	h.Record(3.5) // overflow -> returns last bound

	if got, ok := h.QuantileUpperBound(0.5); !ok || got != 2 {
		t.Fatalf("p50: got (%v,%v) want (2,true)", got, ok)
	}
	if got, ok := h.QuantileUpperBound(0.95); !ok || got != 3 {
		t.Fatalf("p95: got (%v,%v) want (3,true)", got, ok)
	}
}

func TestHistogramReset(t *testing.T) {
	t.Parallel()

	h := NewHistogram([]float64{1})
	h.Record(0.5)
	if h.Count() != 1 {
		t.Fatalf("expected count 1, got %d", h.Count())
	}

	h.Reset()
	if h.Count() != 0 {
		t.Fatalf("expected count 0, got %d", h.Count())
	}
	if _, ok := h.QuantileUpperBound(0.5); ok {
		t.Fatalf("expected no quantile after reset")
	}
}
