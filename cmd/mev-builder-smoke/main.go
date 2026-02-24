package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/mev/builderendpoint"
	"github.com/ssvlabs/ssv/mev/builderendpoint/config"
)

func main() {
	var (
		listen              = flag.String("listen", ":18550", "listen address")
		relaysCSV           = flag.String("relays", "", "comma-separated relay URLs (e.g. http://relay_a:18551,http://relay_b:18552)")
		relayTimeout        = flag.Duration("relay-timeout", 750*time.Millisecond, "per-relay request timeout")
		bidDeadline         = flag.Duration("bid-deadline", 750*time.Millisecond, "overall bidding deadline")
		bidGap              = flag.Duration("bid-gap", 50*time.Millisecond, "polling gap between bid attempts")
		cacheTTL            = flag.Duration("cache-ttl", 2*time.Second, "bid cache TTL")
		prefetchMaxInflight = flag.Int("prefetch-max-in-flight", 4, "max in-flight prefetch requests")
		unblindRetries      = flag.Int("unblind-retries", 0, "retries per relay for unblinding")
		unblindRetryInt     = flag.Duration("unblind-retry-interval", 0, "interval between unblind retries")
	)
	flag.Parse()

	if *relaysCSV == "" {
		log.Fatal("-relays is required")
	}
	relays := splitCSV(*relaysCSV)
	if len(relays) == 0 {
		log.Fatal("no relays supplied")
	}

	logger, err := zap.NewDevelopment()
	if err != nil {
		log.Fatalf("logger: %v", err)
	}

	cfg := config.Config{
		Enabled:              true,
		ListenAddress:        *listen,
		Relays:               relays,
		RelayRequestTimeout:  *relayTimeout,
		BidDeadline:          *bidDeadline,
		BidGap:               *bidGap,
		CacheTTL:             *cacheTTL,
		PrefetchMaxInFlight:  *prefetchMaxInflight,
		UnblindRetries:       *unblindRetries,
		UnblindRetryInterval: *unblindRetryInt,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv, err := builderendpoint.New(ctx, logger, cfg, builderendpoint.Dependencies{
		// For this smoke harness we don't have a real chain time; make deadlines relative to "now".
		SlotStartTime: func(_ phase0.Slot) time.Time { return time.Now() },
	})
	if err != nil {
		log.Fatalf("builder endpoint init: %v", err)
	}

	if err := srv.Run(ctx); err != nil {
		log.Fatalf("builder endpoint error: %v", err)
	}
}

func splitCSV(in string) []string {
	parts := strings.Split(in, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}
