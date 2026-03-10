package bids

import (
	"context"
	"math/big"
	"time"

	builderspec "github.com/attestantio/go-builder-client/spec"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/metrics"

	"github.com/ssvlabs/ssv/mev/builderendpoint/bidcache"
	"github.com/ssvlabs/ssv/mev/builderendpoint/relayurl"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/mev/builderendpoint/bids"
	observabilityNamespace = "ssv.mev.builder_endpoint"
)

type bidFetchResult string

const (
	bidFetchResultBid   bidFetchResult = "bid"
	bidFetchResultNoBid bidFetchResult = "no_bid"
	bidFetchResultError bidFetchResult = "error"
)

var (
	meter = otel.Meter(observabilityName)

	bidFetchesCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "bid_fetches"),
			metric.WithUnit("{fetch}"),
			metric.WithDescription("number of relay bid fetch operations")))

	bidFetchDurationHistogram = metrics.New(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "bid_fetch.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("duration of relay bid fetch operations in seconds"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))

	bidFetchWinnersCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "bid_fetch.winners"),
			metric.WithUnit("{winner}"),
			metric.WithDescription("number of times a relay won bid selection")))

	bidFetchWinningValueETHHistogram = metrics.New(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "bid_fetch.winning_value_eth"),
			metric.WithUnit("ETH"),
			metric.WithDescription("value of winning bids selected by the builder endpoint (in ETH)")))
)

func bidFetchAttributes(source string, res bidFetchResult) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("ssv.mev.builder_endpoint.bid_fetch.source", source),
		attribute.String("ssv.mev.builder_endpoint.bid_fetch.result", string(res)),
	}
}

func bidValueETH(bid *builderspec.VersionedSignedBuilderBid) (float64, bool) {
	if bid == nil {
		return 0, false
	}
	valueWei, err := bid.Value()
	if err != nil || valueWei == nil {
		return 0, false
	}

	weiAsFloat := new(big.Float).SetInt(valueWei.ToBig())
	ethAsFloat := new(big.Float).Quo(weiAsFloat, big.NewFloat(1e18))
	eth, _ := ethAsFloat.Float64()
	return eth, true
}

// FetcherWithMetrics wraps a bidcache.Fetcher to record bid fetch timings and outcomes.
type FetcherWithMetrics struct {
	Source string
	Next   bidcache.Fetcher

	Observer WinningBidObserver
}

// WinningBidObserver receives the winning bid value selected by the fetcher.
// Implementations must be low-overhead and thread-safe.
type WinningBidObserver interface {
	ObserveWinningBid(source string, relayHost string, valueETH float64)
}

func (f *FetcherWithMetrics) FetchBestBid(ctx context.Context, key bidcache.Key) (*builderspec.VersionedSignedBuilderBid, string, error) {
	if f == nil || f.Next == nil {
		return nil, "", nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	start := time.Now()
	bid, provenance, err := f.Next.FetchBestBid(ctx, key)
	duration := time.Since(start)

	result := bidFetchResultNoBid
	if err != nil {
		result = bidFetchResultError
	} else if bid != nil {
		result = bidFetchResultBid
	}

	attr := bidFetchAttributes(f.Source, result)
	bidFetchesCounter.Add(ctx, 1, metric.WithAttributes(attr...))
	bidFetchDurationHistogram.Record(ctx, duration.Seconds(), metric.WithAttributes(attr...))

	if err == nil && bid != nil && provenance != "" {
		relayHost := relayurl.Host(provenance)

		// Relay addresses are configured and low-cardinality for a given operator.
		bidFetchWinnersCounter.Add(ctx, 1, metric.WithAttributes(append(attr, attribute.String("ssv.mev.builder_endpoint.relay", relayHost))...))

		if eth, ok := bidValueETH(bid); ok {
			valAttr := append(attr, attribute.String("ssv.mev.builder_endpoint.relay", relayHost))
			bidFetchWinningValueETHHistogram.Record(ctx, eth, metric.WithAttributes(valAttr...))

			if f.Observer != nil {
				f.Observer.ObserveWinningBid(f.Source, relayHost, eth)
			}
		}
	}

	return bid, provenance, err
}
